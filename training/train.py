"""Fine-tunes Qwen3 4B Instruct 2507 on data/train.jsonl with a QLoRA adapter.

Only the last message of each conversation, the assistant's reply, is learned. The adapter is
written to out/adapter. Sized for an 8 GB GPU without bf16 (an RTX 20 series).
"""

import argparse
import json
import os

os.environ.setdefault("PYTORCH_ALLOC_CONF", "expandable_segments:True")  # torch 2.9's name
os.environ.setdefault("PYTORCH_CUDA_ALLOC_CONF", "expandable_segments:True")

import torch
from datasets import Dataset
from huggingface_hub import snapshot_download
from peft import LoraConfig
from transformers import AutoModelForCausalLM, AutoTokenizer, BitsAndBytesConfig
from transformers.integrations import sdpa_attention
from trl import SFTConfig, SFTTrainer

BASE = "Qwen/Qwen3-4B-Instruct-2507"  # Apache 2.0


def load(path, tok, max_len):
    rows, dropped = [], 0
    for line in open(path):
        msgs = json.loads(line)["messages"]
        if len(tok.apply_chat_template(msgs, tokenize=True)) > max_len:
            dropped += 1
            continue
        rows.append({"prompt": msgs[:-1], "completion": msgs[-1:]})
    print(f"{path}: {len(rows)} examples, {dropped} too long")
    return Dataset.from_list(rows)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--epochs", type=float, default=1)
    p.add_argument("--lr", type=float, default=2e-4)
    p.add_argument("--rank", type=int, default=16)
    p.add_argument("--max-len", type=int, default=2560)  # longer ones run an 8 GB GPU out of memory
    p.add_argument("--data", default="data/train.jsonl")
    p.add_argument("--out", default="out/adapter")
    args = p.parse_args()

    # The math attention kernel holds every head's full score matrix in fp32, 800 MB at 2560
    # tokens, which an 8 GB GPU can't spare. It is picked whenever SDPA is asked for grouped-query
    # attention on a GPU without flash attention (an RTX 20), so repeat the KV heads instead and
    # leave only the memory-efficient kernel.
    sdpa_attention.use_gqa_in_sdpa = lambda *_, **__: False
    torch.backends.cuda.enable_math_sdp(False)

    base = snapshot_download(BASE)  # a local path, so nothing asks the Hub again
    tok = AutoTokenizer.from_pretrained(base)
    train = load(args.data, tok, args.max_len)

    # torch counts emulated bf16 as supported; an RTX 20 has only fp16 natively
    fp16 = not torch.cuda.is_bf16_supported(including_emulation=False)
    dtype = torch.float16 if fp16 else torch.bfloat16
    model = AutoModelForCausalLM.from_pretrained(
        base,
        quantization_config=BitsAndBytesConfig(
            load_in_4bit=True,
            bnb_4bit_quant_type="nf4",
            bnb_4bit_compute_dtype=dtype,
            bnb_4bit_use_double_quant=True,
        ),
        dtype=dtype,
        attn_implementation="sdpa",
    )

    cfg = SFTConfig(
        output_dir=args.out,
        num_train_epochs=args.epochs,
        learning_rate=args.lr,
        lr_scheduler_type="cosine",
        warmup_ratio=0.03,
        per_device_train_batch_size=1,
        gradient_accumulation_steps=8,
        gradient_checkpointing=True,
        torch_empty_cache_steps=1,  # the longest examples fit only without fragmentation
        use_liger_kernel=True,  # fused loss: the 150k-token vocabulary's logits don't fit otherwise
        max_length=args.max_len,
        completion_only_loss=True,
        fp16=fp16,
        bf16=not fp16,
        optim="paged_adamw_8bit",
        logging_steps=10,
        save_strategy="steps",
        save_steps=100,
        save_total_limit=2,
        report_to="none",
        seed=7,
    )
    lora = LoraConfig(
        r=args.rank,
        lora_alpha=2 * args.rank,
        lora_dropout=0.05,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"],
        task_type="CAUSAL_LM",
    )
    trainer = SFTTrainer(model=model, args=cfg, train_dataset=train, processing_class=tok, peft_config=lora)
    trainer.train()
    trainer.save_model(args.out)
    print("adapter written to", args.out)


if __name__ == "__main__":
    main()

"""Merges out/adapter into the base model and writes out/gorun-qwen3-4b-q4_k_m.gguf.

Needs llama.cpp's source in .llama.cpp (for its converter) with llama-quantize built; see
README.md.
"""

import argparse
import subprocess
import sys
from pathlib import Path

import torch
from huggingface_hub import snapshot_download
from peft import PeftModel
from transformers import AutoModelForCausalLM, AutoTokenizer

BASE = "Qwen/Qwen3-4B-Instruct-2507"
LLAMA = next(Path(".llama.cpp").glob("llama.cpp-*"))


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--adapter", default="out/adapter")
    p.add_argument("--name", default="gorun-qwen3-4b")
    args = p.parse_args()

    base = snapshot_download(BASE)
    merged = Path("out/merged")
    model = AutoModelForCausalLM.from_pretrained(base, dtype=torch.float16)
    model = PeftModel.from_pretrained(model, args.adapter).merge_and_unload()
    model.save_pretrained(merged)
    AutoTokenizer.from_pretrained(base).save_pretrained(merged)

    f16 = Path(f"out/{args.name}-f16.gguf")
    q4 = Path(f"out/{args.name}-q4_k_m.gguf")
    subprocess.run([sys.executable, LLAMA / "convert_hf_to_gguf.py", merged, "--outtype", "f16", "--outfile", f16], check=True)
    subprocess.run([LLAMA / "build/bin/llama-quantize", f16, q4, "Q4_K_M"], check=True)
    f16.unlink()
    print("wrote", q4)


if __name__ == "__main__":
    main()

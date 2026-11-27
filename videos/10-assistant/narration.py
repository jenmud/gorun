"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! Today, we're going to meet gorun's built-in assistant. "
        "It's a small language model, trained on gorun itself, that runs inside the web dashboard, on your own machine.\n"
        "It can explain why a run failed, summarise your runs, answer questions about your Gorunfile, write small tasks for you, and set up schedules.\n"
        "Before we start, the important part: it's experimental. It's an early version, it needs a lot more training, "
        "and it can still get things wrong. So read everything it writes.\n"
        "Same synthetic voice as before. The servers are stand-ins that only sleep, but every answer in this video is the model's own."
    ),
    "start": (
        "There's nothing to install. Start the dashboard with gorun dash serve, as usual, and open the Assistant tab.\n"
        "The first question downloads what it needs: llama dot c p p, which runs the model, and the model itself, "
        "gorun's own fine-tune of Qwen three, four B, about two and a half gigabytes, under the Apache license. "
        "It's kept in gorun's cache folder, so that only happens once. It uses your graphics card when there's room, "
        "and the Model panel shows where it's running, and how fast.\n"
        "And it's private. The model runs on this machine, so your Gorunfile and your run history never leave it. "
        "Your environment variables and template variables are never sent to the model at all."
    ),
    "diagnose": (
        "Let's break something. Here, nginx failed on web three, so its site and smoke test were skipped. "
        "On the Live page, or the run's History page, press Ask the assistant.\n"
        "It reads the failed task's command, its exit code and the end of its output, and tells you what went wrong: "
        "port eighty is already in use on web three. Then it suggests how to fix it."
    ),
    "summary": (
        "It can summarise, too. Ask it to sum up the recent runs, and you get what ran, what failed, and where, "
        "instead of clicking through every run.\n"
        "And it knows your Gorunfile. Ask which tasks run on which servers, and it lists each server's tasks, in the order they run."
    ),
    "write": (
        "Now let's have it write some tasks. This is what we trained it hardest on.\n"
        "Ask for redis, installed and kept running, and it writes small tasks, one step each: install it, then enable and start it. "
        "Every task gets a check, so it's skipped when the step is already done, and a verify, so it fails if the step didn't work.\n"
        "For something bigger, like postgres with a nightly backup, it splits the job into small tasks: "
        "install, keep it running, make the backup folder, then the backup. "
        "gorun checks that every task loads, and sends a reply back to the model if a task is missing its check or verify.\n"
        "Press Save, and they're in your Gorunfile straight away, ready on the Tasks page.\n"
        "But read them first. Look at the backup's check: it skips the backup whenever a backup file already exists. "
        "So after the first night, it would never run again. That's exactly the kind of mistake a small model still makes."
    ),
    "schedules": (
        "It sets up schedules too. That postgres answer came with one for the backup. "
        "But read it closely: we asked for one in the morning, and it wrote half past one, in Sydney time, which nobody asked for. "
        "And it can change the ones you have. Ask it to move nightly disk to half past four, Sydney time, "
        "and it rewrites that schedule under the same name, with only the time and zone changed.\n"
        "gorun checks every schedule the way it checks the Gorunfile: the target has to exist, and the timing and time zone have to be real. "
        "Save, and it's on the Schedules page, where you can still pause it, or skip a run.\n"
        "And look: gorun has caught a mistake. Our demo servers are stand-ins on this machine, "
        "and the model forgot to mark the backup task local, so the page warns that every run will fail. "
        "Another reminder that it's experimental."
    ),
    "generate": (
        "The Config page has the same thing as a form. Open Generate tasks, describe what you need, "
        "like backing up a folder every night, and press Generate. "
        "The tasks it writes are checked, and land in your unsaved changes, for you to review before you save."
    ),
    "experimental": (
        "So, how good is it? On our tests, it writes working tasks about eight times out of ten, "
        "far better than the model it started from, and it gets schedules right seven times out of eight. "
        "But it's weak on general questions about gorun. gorun now hands it the matching sections of its docs, "
        "and it still makes up options and settings that don't exist. "
        "Check those against the docs, on go dash run dot dev.\n"
        "It needs much more training, and it'll get better with every version. "
        "It never runs anything or changes your servers by itself."
    ),
    "models": (
        "You can also use other models. The new Models panel, under the Model panel, lists every model on this machine. "
        "Open switches to one and loads it, Close frees its memory, and Delete removes one you don't need. "
        "Once you've added users, only an admin can do this. And since everyone shares the model, it won't switch while someone is still chatting, unless you say so.\n"
        "Search finds models on Hugging Face, any GGUF model llama.cpp can run, with their licences. "
        "Open one to see its sizes, smallest first, and Download one. It downloads in the background, "
        "and is checked against the digest Hugging Face publishes before you can open it. "
        "Just remember: gorun's prompts are tuned for its own model, so others usually answer worse.\n"
        "You can also point G O R U N L L M model at a model file, or a link to one.\n"
        "Or set G O R U N L L M U R L to a model server, like Ollama, L M Studio, or a hosted one. "
        "If that server isn't on your machine, the page warns you, in amber, that your Gorunfile and run output are sent there.\n"
        "And one model serves everyone. Questions wait in a queue, so more people never costs more memory, just a short wait."
    ),
    "training": (
        "And here's how you can help it get better. On the Assistant page, an admin can open Training data, and turn on capture. "
        "It's off until you do, and while it's on, the page tells everyone who uses it.\n"
        "Every answer is then kept in your history database: the prompt exactly as the model saw it, the reply, "
        "whether its tasks loaded, whether you saved them, and how they ran afterwards. Each answer gets a thumbs up and a thumbs down, so you can say whether it was right.\n"
        "Export J S O N L downloads it all, ready for training, with addresses and keys replaced. Nothing is sent anywhere unless you send it."
    ),
    "outro": (
        "So that's the assistant. It explains failures, summarises runs, writes small tasks with a check and a verify, and sets up schedules, "
        "all on your own machine.\n"
        "Remember, it's experimental, so review every task before you run it.\n"
        "Thanks so much for watching, and I'll see you next time!"
    ),
}

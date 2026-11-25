"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! Today, we're going to meet gorun's built-in assistant. "
        "It's a small language model that runs inside the web dashboard, on your own machine.\n"
        "It can explain why a run failed, summarise your runs, answer questions about your Gorunfile, and write new tasks for you.\n"
        "Same synthetic voice as before. The servers are stand-ins that only sleep, but every answer in this video is the model's own."
    ),
    "start": (
        "There's nothing to install. Start the dashboard with gorun dash serve, as usual, and open the Assistant tab.\n"
        "The first question downloads what it needs: llama dot c p p, which runs the model, and the model itself, "
        "Qwen three, four B Instruct, about two and a half gigabytes, under the Apache license. "
        "They're kept in gorun's cache folder, so that only happens once. It uses your graphics card when it finds one.\n"
        "And the page tells you it's private. The model runs on this machine, so your Gorunfile and your run history never leave it. "
        "Your environment variables and template variables are never sent to the model at all."
    ),
    "diagnose": (
        "Let's break something. Here, nginx failed on web three, so its site and smoke test were skipped. "
        "On the run's History page, press Ask the assistant.\n"
        "It reads the failed task's command, its exit code and the end of its output, and tells you what went wrong: "
        "port eighty is already in use on web three. Then it suggests how to fix it."
    ),
    "summary": (
        "It can summarise, too. Ask it to sum up the recent runs, and you get what ran, what failed, and where, "
        "instead of clicking through every run.\n"
        "And it knows your Gorunfile. Ask which tasks run on which servers, and it lists each server's tasks, in the order they run."
    ),
    "write": (
        "Now let's have it write a task. Ask for one that installs redis and keeps it running.\n"
        "It writes a check, so the task is skipped when redis is already running, and a verify, so the task fails if redis didn't start. "
        "gorun checks that the task loads, and offers to add it. "
        "Press Add as unsaved changes, and it waits on the Config page for you to review it. Nothing is saved until you press Save."
    ),
    "generate": (
        "The Config page has the same thing as a form. Open Generate tasks, describe what you need, "
        "like backing up a folder every night, and press Generate. "
        "The tasks it writes are checked, and land in your unsaved changes too.\n"
        "Read every one before you save. It's a small model, so commands can be wrong, "
        "and paths or passwords may need filling in."
    ),
    "models": (
        "You can also use other models. Point G O R U N L L M model at another model file, or a link to one, and gorun runs that instead.\n"
        "Or set G O R U N L L M U R L to a model server, like Ollama, L M Studio, or a hosted one. "
        "If that server isn't on your machine, the page warns you, in amber, that your Gorunfile and run output are sent there."
    ),
    "outro": (
        "So that's the assistant. It explains failures, summarises runs, answers questions about your Gorunfile, and writes tasks, "
        "all on your own machine.\n"
        "It never runs anything or changes your servers by itself, and you review every task before it's saved.\n"
        "Thanks so much for watching, and I'll see you next time!"
    ),
}

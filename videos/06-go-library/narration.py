"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! So far, we've used gorun as a command. Today, we're going to use it as a library, "
        "from your own Go code.\n"
        "Everything you've seen, the tasks, the servers, the events and the history, is a Go package that the command is built on. "
        "So you can run a Gorunfile from your own program, and show the progress your own way.\n"
        "Same synthetic voice as before. The servers in this demo are local, and only echo and sleep, "
        "but the code and the output are the real thing."
    ),
    "why": (
        "Why would you do that? Because sometimes the picker and the live view aren't the right shape.\n"
        "Maybe you want a button in a web app that restarts a service. Or a release tool with its own progress bars. "
        "Or a bot that runs a task when someone asks.\n"
        "You keep the Gorunfile, and gorun still does the running. Your program decides what to run, and what to do with the results.\n"
        "If a command is all you need, stay with the command. The library is for when you're building something around it."
    ),
    "hello": (
        "Add it to your module with go get, and import the gorun package.\n"
        "Here's the smallest program. Load the Gorunfile. Make its relative paths relative to the file. And run it.\n"
        "That runs every server's tasks at once, over SSH or locally, exactly like the command with dash dash run. "
        "If any task fails, run returns an error.\n"
        "One thing to know: gorun logs every task to standard output, which gets noisy. "
        "So this program sends the logs away with set log output. A program that draws on the terminal should always do that."
    ),
    "events": (
        "Running quietly is a start, but the interesting part is following a run as it happens. "
        "Set on event, and gorun calls your function for every event.\n"
        "A task event arrives each time a task changes state: running, done, failed, or skipped, with the output and the exit code. "
        "A process finished event arrives at the end, with the totals.\n"
        "Here we run one task, restart, and the tasks it depends on. Web two's deploy fails, so its restart is skipped, "
        "and the other servers carry on.\n"
        "Notice the mutex. Every server runs in its own goroutine, so your function is called from many at once. Lock whatever it updates."
    ),
    "select": (
        "You choose what runs, the same way the command-line flags do. "
        "Select servers picks servers by tag, group or name, like the dash dash tag flag. "
        "Expand task patterns turns names, globs and at-bundle references into task names, like dash dash task.\n"
        "Set those names on the servers you picked, and pass them to run. "
        "Here, the rollout bundle runs only on the web servers.\n"
        "And run task runs one task, with everything it needs, on some or all of your servers."
    ),
    "history": (
        "The library doesn't save runs by itself. It hands every event to a recorder, and the history package has one that saves runs to SQLite.\n"
        "Open it, and set it as the config's recorder. It's the same database that the command and the web dashboard use, "
        "so a run from your program shows up in gorun dash history, and on the dashboard's History page.\n"
        "And you can read the runs back, to show history in your own app: the list of runs, one run with its tasks, "
        "or a single task's output and exit code.\n"
        "If you'd rather keep runs somewhere else, a recorder is just one method, and a plain function will do."
    ),
    "encode": (
        "One more trick. Config dot encode writes a config back out as a Gorunfile.\n"
        "Load a task library, add a server built in code, and encode it. "
        "Here's the new server's section: the host, the task list, and the login.\n"
        "It's the inverse of loading, so a program can create or change a Gorunfile, and you can show it, save it, or diff it. "
        "Secrets are written exactly as they are, so keep real ones in the environment.\n"
        "Scheduling is in the library too. New scheduler fires your schedules, and you can pause or skip them, all from code."
    ),
    "examples": (
        "Two complete examples live in the examples folder of the repository.\n"
        "Rollout is a command-line tool that draws its own progress bars. One bar per server, the task running now, retries, "
        "and a summary, all drawn from those events. Here, edge four fails to install, so the run finishes with one failure and some skipped tasks.\n"
        "And Service Deck is a small web app. Pick a group of servers, press an action, and every server's progress streams to the browser live. "
        "Each button is just a gorun task.\n"
        "Both are short, and a good place to start your own."
    ),
    "outro": (
        "So that's the library: load a Gorunfile, choose what runs, follow the events, and record the history.\n"
        "The package documentation covers all of it, with examples you can run, and the examples folder has the two programs.\n"
        "In the next video, we'll look at observability: sending traces and metrics with OpenTelemetry, and the events that gorun publishes on NATS.\n"
        "Thanks so much for watching, and I'll see you there!"
    ),
}

"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! So far, we've used gorun from the terminal. Today, we're going to meet its web dashboard.\n"
        "It lets you run tasks from a browser, watch runs live, browse the history of every run, and edit your tasks and schedules. "
        "And it's what keeps your schedules running.\n"
        "Same synthetic voice as before. The servers in this demo are stand-ins on one machine that only sleep, "
        "but the dashboard is the real thing."
    ),
    "start": (
        "To start it, run gorun dash serve, and the name of your Gorunfile. That's the whole setup. "
        "It prints where to find it: open localhost, port eight thousand and eighty.\n"
        "It also starts a small NATS server of its own, which is how runs send their events to the dashboard, "
        "and it stores every run in a history database. So live updates and history just work, with nothing else to install.\n"
        "One important warning. The dashboard has no login, so anyone who can reach the port can run tasks. "
        "Keep it on a trusted network, or bind it to localhost with G O R U N address, and reach it through an S S H tunnel."
    ),
    "run": (
        "Let's run something. The Run page is the same picker we used in the terminal. "
        "Tick the servers, then either a bundle or some tasks, and press Run selected. "
        "Force skips the checks, and Check only is the dry run we saw before.\n"
        "The page jumps to Live. Each run, and each server in it, starts collapsed, with a progress bar. "
        "Open a server to see its tasks, with the one that's running right now.\n"
        "When it finishes, the failed server is marked in red. Here, the smoke test on web three failed. "
        "Stop cancels a run that's still going."
    ),
    "history": (
        "Every run is saved. The History page lists them newest first, with the result, how long each took, "
        "and who started it. Open one, and you get every server's tasks, slowest first.\n"
        "Open the failed task, and there's the exit code, the error output, and the exact command that ran. "
        "That's the command you can copy, and run by hand on the server.\n"
        "Every run also records where it came from: the user, the machine, the gorun version, and whether it was started "
        "from the terminal, the web, a schedule, or C I."
    ),
    "terminal": (
        "And runs from other places show up here too. Start a run from a terminal on the same machine, "
        "or from a C I job that can reach the same NATS server, and it appears on the Live page, and in the history. "
        "So the dashboard can be the one place your team looks."
    ),
    "tasks": (
        "The other pages are for looking around, and for editing. Tasks lists every task, slowest first. "
        "Open one for its command, its copies, its check and its verify, what it depends on, which bundles include it, and which servers run it. "
        "The filter box searches all of that.\n"
        "Add a task, and fill in the form. It's written straight into your Gorunfile, and comments and layout elsewhere in the file are kept. "
        "Bundles work the same way."
    ),
    "schedules": (
        "The Schedules page is where the scheduler shows itself. Each schedule has its target, its timing and its next run. "
        "You can pause one, resume it, or skip just the next run, and add or edit schedules with a form. "
        "Because the dashboard is what runs the scheduler, it needs to stay up for schedules to fire.\n"
        "A scheduled run is marked with a badge, in the live view and in the history, so you can see which runs nobody started by hand."
    ),
    "outro": (
        "So that's the dashboard. Start it with gorun dash serve, run and watch from the browser, "
        "look back through the history, and edit tasks and schedules, all in one place.\n"
        "Remember, it has no login, so keep it on a network you trust.\n"
        "Thanks so much for watching, and I'll see you next time!"
    ),
}

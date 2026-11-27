"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! So far, we've used gorun from the terminal. Today, we're going to meet its web dashboard.\n"
        "It lets you run tasks from a browser, watch runs live, browse the history of every run, and edit your tasks and schedules. "
        "And it's new: it can now keep your Gorunfiles in its own database, with every version saved.\n"
        "Same synthetic voice as before. The servers in this demo are stand-ins on one machine that only sleep, "
        "but the dashboard is the real thing."
    ),
    "start": (
        "To start it, run gorun dash serve. That's the whole setup, and it's the main way to use gorun. "
        "It keeps your Gorunfiles in its history database, starting with an empty one called default, and every save is a version. "
        "Already have a Gorunfile in git? Give it the name, and it runs and edits that file instead. "
        "Open localhost, port eight thousand and eighty.\n"
        "It also starts a small NATS server of its own, which is how runs send their events to the dashboard. "
        "So live updates and history just work, with nothing else to install.\n"
        "One important warning. Out of the box, the dashboard has no login, so anyone who can reach the port can run tasks. "
        "The startup message says so. We'll add users at the end of this video. "
        "Or bind it to localhost with G O R U N address, and reach it through an S S H tunnel."
    ),
    "gorunfiles": (
        "Let's fill it in. On the Config page, Import takes a Gorunfile from your computer, or a zip. "
        "It's checked to load before anything changes, and the one before is kept as an earlier version.\n"
        "The Gorunfiles page lists every one kept here. We've got two: web, our fleet, and base, with shared tasks. "
        "Web uses base's tasks with an include line, stored, colon colon, base. "
        "Edit a shared task once, and every Gorunfile that includes it gets the change. "
        "And every Gorunfile's schedules run, whichever one is open.\n"
        "Every save is a new version, and Export as zip downloads it, one file per task, ready to load anywhere. "
        "And if you only want to watch, gorun dash serve dash live shows just the Live, History and Assistant pages."
    ),
    "run": (
        "Let's run something. The Live page opens with stats, and a bar for each recent run. "
        "The Run page is the same picker we used in the terminal. "
        "Tick the servers, then either a bundle or some tasks, and press Run selected.\n"
        "The page jumps to Live. Each run, and each server in it, starts collapsed, with a progress bar. "
        "Open a server to see its tasks, with the one that's running right now.\n"
        "When it finishes, the failed server is marked in red. Here, the smoke test on web three failed. "
        "Stop cancels a run that's still going."
    ),
    "history": (
        "Every run is saved. The History page lists them newest first, with the result, how long each took, "
        "and who started it. Open one, and you get every server's tasks, slowest first.\n"
        "Open the failed task, and there's the exit code, the error output, and the exact command that ran. "
        "That's the command you can copy, and run by hand on the server."
    ),
    "terminal": (
        "And runs from other places show up here too. Start a run from a terminal on the same machine, "
        "or from a C I job that can reach the same NATS server, and it appears on the Live page, and in the history. "
        "So the dashboard can be the one place your team looks."
    ),
    "tasks": (
        "Tasks lists every task, slowest first, with badges for the ones that have a check and a verify. "
        "Open one for its command, its check and its verify, what it depends on, which bundles include it, and which servers run it.\n"
        "Add or edit a task with a form. With a stored Gorunfile, every save becomes a new version in the database. "
        "With a file, it's written into the file, and comments elsewhere are kept."
    ),
    "schedules": (
        "The Schedules page is where the scheduler shows itself. Each schedule has its target, its timing and its next run. "
        "You can pause one, resume it, or skip just the next run. "
        "Because the dashboard is what runs the scheduler, it needs to stay up for schedules to fire."
    ),
    "assistant": (
        "And there's a brand new Assistant tab: a small language model, trained on gorun, that runs on your own machine. "
        "It explains failed runs, writes small tasks for you, each with a check and a verify, and sets up schedules.\n"
        "It's experimental, though, and needs a lot more training. It can still make things up, so read everything it writes. "
        "Video ten covers it in detail."
    ),
    "users": (
        "Now, the login. Open the Users page, and add the first user. They're an admin, and you're signed in as them. "
        "From then on, anyone can still look around and ask the assistant, but changing things means signing in.\n"
        "A user can run tasks and edit the Gorunfile. An admin can also manage users, and the assistant's models. "
        "Passwords are kept as bcrypt hashes, in the history database, so with Postgres or MySQL every dashboard on it shares the same users.\n"
        "There's also G O R U N admin password, which signs in as admin, and G O R U N admins, for an auth proxy in front."
    ),
    "outro": (
        "So that's the dashboard. Start it with gorun dash serve, keep your Gorunfiles in its database, "
        "run and watch from the browser, and look back through the history.\n"
        "Remember to add a user, or keep it on a network you trust.\n"
        "Thanks so much for watching, and I'll see you next time!"
    ),
}

"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! So far, one Gorunfile has been enough. Today, we're going to scale up.\n"
        "We'll cover three things. Splitting a big Gorunfile into smaller files. "
        "Sharing tasks between projects, from a git repository. And running tasks on a schedule.\n"
        "Same synthetic voice as before, and everything you see is the real thing, running for real."
    ),
    "problem": (
        "A Gorunfile that works for five servers tends to grow. Soon it has dozens of tasks, and a hundred servers, "
        "and finding anything means scrolling.\n"
        "Then a second project needs the same tasks, and you copy and paste them, and the copies drift apart.\n"
        "And some jobs, like a nightly backup, shouldn't wait for somebody to remember to run them."
    ),
    "split1": (
        "Let's start with splitting. A Gorunfile can have a top level include list, with files or globs, "
        "and they're relative to the file that names them.\n"
        "Here, shared tasks go in a tasks folder, server groups in a groups folder, and the hosts in their own files. "
        "gorun merges them all into one config, so a name from any file works anywhere."
    ),
    "split2": (
        "The pieces are plain Gorunfile sections. The web group lives in its own file, "
        "with its tags, its tasks and how to log in. And the hosts are one short line each.\n"
        "To see the result, ask gorun. List servers shows every server, with its groups, its tags, "
        "and the tasks it will run, in order. Nothing is contacted, so it's safe to run any time."
    ),
    "splitrules": (
        "A few rules keep this tidy. If a name is defined in two files, that's an error, and the message names both files. "
        "So the order of the files can never change a value.\n"
        "Only the root Gorunfile sets things that are global, like the title, the logging and the SSH defaults. "
        "A pattern that matches nothing is an error, and so is an include that loops back.\n"
        "Included files can include other files too. And paths inside them, like copy sources, are relative to the root Gorunfile."
    ),
    "gitintro": (
        "Now, sharing. Say you have a handful of tasks that every project needs. Write them once, "
        "keep them in a git repository, and include them with a git source.\n"
        "A library is just a repository of TOML files, with tasks and bundles in them. "
        "Ours has one file, with a task that writes a message of the day, and a bundle called baseline.\n"
        "In the Gorunfile, the include starts with git, then the repository, then a double slash, and the files you want. "
        "And a ref is required: a tag, a branch or a commit. So the Gorunfile always says which version it wants.\n"
        "Here, the repository is on this machine, so we can try it. Yours would be an https or S S H address."
    ),
    "gitrun": (
        "Let's run it. gorun fetches the library, and runs its tasks like any others. "
        "And it writes a file next to the Gorunfile, called Gorunfile dot lock, with the exact commit the ref turned into. Commit it.\n"
        "Now the library moves on, and someone adds a disk task to the bundle. Our bundle still has two tasks, "
        "because the lock file pins the commit. A moved branch changes nothing, until you ask.\n"
        "Dash dash update includes looks every ref up again, rewrites the lock file, and prints the new commit. "
        "Now the bundle has three tasks, and the next run uses them. The first one is unchanged, because its check passed."
    ),
    "gitsafe": (
        "Is it safe to run somebody else's tasks? There are guard rails. Tasks from a git repository can only be tasks and bundles. "
        "They can't add servers, groups or schedules, so a library never changes where, when or with what your commands run.\n"
        "Here's a branch of our library that tries to sneak in a server. gorun refuses to load it.\n"
        "Credentials are git's own, so there's nothing secret in the Gorunfile. Each commit is cached, so with the lock file, "
        "gorun works offline. And every run records which commits it used, so an old run can always be traced to the exact commands."
    ),
    "schedintro": (
        "Now, schedules. A schedule table runs a task or a bundle on every server, on a cron pattern, or once at a set time.\n"
        "This one runs the nightly bundle at two in the morning, in Sydney. Cron has five fields: minute, hour, day, month and weekday. "
        "The time zone is a name, and daylight saving follows its rules.\n"
        "And missed run is run, which means if the run fell due while the schedule was paused, or the server was down, "
        "gorun runs it once to catch up. The default is skip.\n"
        "The second schedule is a one off, with an at time, for something like a maintenance window."
    ),
    "schedrun": (
        "Schedules run inside the web service, started with gorun dash serve. "
        "Here's the Schedules page, with each schedule, its target, its timing and when it's next due. "
        "There are buttons to pause, resume, or skip just the next run, for a maintenance window.\n"
        "A plain run from the command line never fires schedules, but it does check them. A bad cron pattern, "
        "like this one with four fields, stops the load with a clear message.\n"
        "Checks work in scheduled runs too, so a backup with a check runs at most once a day, however often it's started. "
        "And every scheduled run is saved in the history, with a badge that links back to its schedule."
    ),
    "outro": (
        "So that's how a Gorunfile grows up. Split it with include. Share tasks from git, pinned by a lock file. "
        "And put the recurring work on a schedule.\n"
        "Everything in this series is in the gorun repository, with the Gorunfiles and the scripts that made these videos, "
        "so you can follow along.\n"
        "Thanks so much for watching, and I'll see you next time!"
    ),
}

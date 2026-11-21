"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! Last time, we wrote a Gorunfile, and used the terminal interface to run it. "
        "Today, we're going to make that Gorunfile reliable.\n"
        "We'll cover three things. Checks, which skip work that's already done, and prove that a change worked. "
        "Secrets, which stay out of your file. And templates, which give every server its own settings.\n"
        "We'll try each one on its own, for real, and then put them together on our web fleet."
    ),
    "problems": (
        "So what goes wrong when you use a Gorunfile for real?\n"
        "First, you run it again, and it repeats everything, even though most of it was already done. "
        "Second, a command can finish without the thing actually working. "
        "Third, the moment you need a password or a token, you're tempted to paste it into the file. "
        "And fourth, no two servers are quite the same. Staging has different settings to production.\n"
        "Checks, verify, secrets and templates are the answer to those four."
    ),
    "checkintro": (
        "Let's start with checks. A task can carry a check, which is a command that runs first, and answers one question: "
        "is there anything to do?\n"
        "If the check exits zero, the answer is no. gorun marks the task unchanged, and skips it. Anything else, and the task runs.\n"
        "Here, the config task writes a file. Its check looks for the line we want. Its command writes it. "
        "And its verify, which runs after the command, looks again, to prove the change took."
    ),
    "checkrun": (
        "Let's run it. The first time, the check fails, because the file isn't there. So the task runs, and gorun says okay, verified.\n"
        "Run it again, and the check passes. The task is unchanged, nothing ran, and the summary says one unchanged. "
        "That's what makes a Gorunfile safe to run again and again.\n"
        "Unchanged counts as a success, so tasks that depend on it still run. And a task with no check always runs."
    ),
    "dryrun": (
        "Two more options help here. Dash dash check is a dry run. Only the checks run, and nothing is copied, installed or changed. "
        "Each task ends up either unchanged, or would run.\n"
        "And dash dash force does the opposite. It skips the checks, and runs everything anyway, "
        "for when you want a task to run whatever its check says. You can't use both together.\n"
        "One important rule: a check should only look. It must never change the machine, because in a dry run, it's the only thing that runs."
    ),
    "verify": (
        "Now, verify. A verify runs after the command, and if it fails, the task fails.\n"
        "Here, I've broken the command on purpose, so it writes version two point three, instead of two point four. "
        "The command itself succeeds, but the verify notices the file is wrong, and the task fails, with the reason: verify failed. "
        "Any tasks that depend on it are skipped.\n"
        "That's how you catch a service that installed fine, but didn't start."
    ),
    "copies": (
        "Checks have a sibling for files. Check copies compares the MD5 of every file a task would copy, with the file already on the server. "
        "If they all match, nothing is copied, and the command doesn't run.\n"
        "In our web fleet, the site task copies a rendered page, and its command only runs when the page changed. "
        "A file that isn't on the server counts as changed. And the comparison costs only a few bytes of traffic, because the server hashes its own files.\n"
        "If a server can't compare, gorun just copies anyway, so it never fails a run."
    ),
    "reliable": (
        "Real servers are slow, flaky, and sometimes still booting. Three settings help.\n"
        "A timeout stops one hung server from holding up everything. A task that runs too long fails with exit code one two four. "
        "Retries try again after a failure, which is great for package locks and network blips, and each attempt gets a fresh timeout.\n"
        "And rerun says whether running a task again is harmless. Mark a task safe if it only converges, like installing a package. "
        "Tasks marked unsafe are never retried automatically."
    ),
    "secrets": (
        "Now, secrets. Never put one in a Gorunfile. Instead, write a dollar sign and the variable's name.\n"
        "gorun fills it in from its own environment, or from a dot env file in the folder you run it from. Keep that file out of git.\n"
        "Here, the token is only a name in the Gorunfile. The real value lives in dot env. "
        "When we run it, the task can use the token, and the output shows only what the task chose to print.\n"
        "gorun sends the values to the task over standard input, so they don't show up in gorun's logs, the history, the events, or the process list on the server."
    ),
    "envvars": (
        "Environment variables don't have to be secrets. Set them globally, on a group, on a server, or on a task, "
        "and the most specific one wins: task, then server, then group, then global.\n"
        "In our fleet, every server is production, except web four, which overrides it to staging. "
        "And a task's command reads them as ordinary shell variables."
    ),
    "templates": (
        "Next, templates, which give every server its own settings. Every setting in a Gorunfile is a template, "
        "filled in for each server before the run starts.\n"
        "Double curly braces and dot name gives the server's name, and dot hostname its address. "
        "Index dot env vars gives one of its environment variables. "
        "And server variables, set on a server, a group or the whole file, are read as dot server dot vars.\n"
        "Here's one task that says which server it is, and each server gets its own answer. "
        "Web one runs production on port eighty eighty, and staging runs staging on port ninety ninety."
    ),
    "render": (
        "Files can be templates too. Add render true to a copy, and gorun fills the file in for each server before it's sent.\n"
        "This config file reads the server's name, its environment, its port, and a task variable called workers. "
        "Task variables belong to the task, and are the same everywhere it runs. Each server gets its own copy.\n"
        "And if a command has double curly braces of its own, like docker ps with a format, set raw true, and gorun leaves it exactly as written."
    ),
    "fleet1": (
        "Let's put it all together on the web fleet. The nginx task checks that nginx is installed and running, and verifies that it's running afterwards. "
        "The site task renders a page, and checks the copy. "
        "The announce task posts to a webhook with a secret token, and says which server it is.\n"
        "First, a dry run, on brand new servers. Every task would run: nineteen tasks, and nothing changed. "
        "Then the real run. All nineteen tasks complete."
    ),
    "fleet2": (
        "Now run it again. Look at the verbose view. Update, smoke and announce run, because they have no check. "
        "But nginx and site say unchanged, with an equals sign, and take no time. Eight of the nineteen tasks had nothing to do.\n"
        "Now suppose we change the page. Only site runs again on every server, and nginx stays unchanged.\n"
        "And a dry run afterwards lists exactly what would run next, without touching anything."
    ),
    "outro": (
        "So that's how you make a deployment reliable. Checks skip what's done, verify proves the change took, "
        "secrets stay out of the file, and templates give every server its own settings.\n"
        "In the next video, we'll scale up: splitting a big Gorunfile across files, sharing tasks between projects, and running on a schedule.\n"
        "Thanks for watching, and I'll see you there!"
    ),
}

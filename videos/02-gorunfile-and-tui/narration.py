"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! In the first video, we looked at what gorun is, and why it exists. "
        "In this one, we're going to learn how to actually use it.\n"
        "We'll write a Gorunfile from scratch, one small step at a time. We'll keep it as a file and run it from the terminal, "
        "the quickest way to see each step. Everything works the same in the web dashboard, which keeps Gorunfiles in its database. "
        "Then we'll take the terminal interface for a proper spin: the picker, the live view, and the history.\n"
        "And just so you know, this is the same synthetic voice as last time. "
        "It lets us keep these tutorials up to date as gorun changes."
    ),
    "concepts": (
        "Before we write anything, let's get five words straight.\n"
        "A task is a shell command with a name. A server is a machine to run it on. "
        "A group holds what servers have in common, like how to log in, or which tasks to run. "
        "A tag is a label, so you can pick servers by it later. "
        "And a bundle is a name for a list of tasks.\n"
        "That's the whole vocabulary. Everything in a Gorunfile is one of those five things."
    ),
    "step1": (
        "Step one: the smallest Gorunfile that does something. Make a folder, and create a file called Gorunfile.\n"
        "First, a task. We'll call it hello, and a task is just a shell command. "
        "We mark it local, which lets it run on your own machine.\n"
        "Next, the servers section lists the tasks that every server runs. We only have one: hello.\n"
        "And finally, one server, called local. It has no SSH settings, so gorun runs it right here, on your own machine."
    ),
    "step1run": (
        "Let's run it. Type gorun, then the name of the file. The picker opens, and nothing runs until you choose it. "
        "Press space to select the server, and then enter to run.\n"
        "There it is. Done.\n"
        "If you don't want the picker, add dash dash run, and gorun runs straight away. "
        "And with dash v, you can see the output too: hello from my laptop."
    ),
    "step2": (
        "Step two: more tasks, in the right order. Tasks on a server run one at a time. "
        "When a task needs another one first, say so with depends on.\n"
        "Here, write file depends on make dir, so make dir always runs first, even though the server only lists hello and write file. "
        "A description shows up in the picker, the live view and the lists, so it's worth adding.\n"
        "Check your work without running anything. Dash dash list tasks shows the dependency tree, "
        "and dash dash list servers shows what each server will run, in order."
    ),
    "step3": (
        "Step three: real servers. A server runs over SSH as soon as it has SSH settings. "
        "And you can leave out anything that's already in your SSH config, like the user, the port or the key.\n"
        "Repeating the login for every server gets old fast, so put it in a server group, once. "
        "Here, a group called ubuntu holds the user and the key, and each server joins the group by name.\n"
        "Notice that the tasks aren't marked local any more. Tasks marked local only run on local servers, "
        "and every other task runs over SSH."
    ),
    "step4": (
        "Step four: tasks that do real work. Sudo true runs the command with sudo, so your login needs passwordless sudo. "
        "A timeout stops one hung server from holding up the whole run, and retries are for flaky networks.\n"
        "To put a file on a server, add a copy: a source on your machine, and a destination on the server. "
        "Copies run before the command, as the login user, and not with sudo. "
        "So we copy to temp first, and then the command moves the file into place.\n"
        "And for longer commands, point cmd file at a script, and gorun sends it over and runs it there."
    ),
    "step5": (
        "Step five: keeping it tidy as you grow. A bundle names a list of tasks, and you use it anywhere a task goes, with an at sign. "
        "Our web app bundle is engine X, the site, and a smoke test. "
        "Dependencies still work: update runs first, even though the bundle doesn't list it.\n"
        "Groups say what servers run. The web group runs the web app bundle, and the db group runs update and disk. "
        "A server's tasks come from its groups.\n"
        "Tags are labels. A server gets the tags of its groups, plus its own, so we can pick out prod or staging later.\n"
        "And when one server is different, change just that server. Web four adds the disk task. "
        "You can skip tasks as well, or replace the list completely."
    ),
    "validate": (
        "Before you run anything, ask gorun what it's going to do. List servers shows each server, its groups and tags, "
        "and the tasks it will run, in order.\n"
        "Notice that the database server only runs update and disk, and web four has the extra disk task. "
        "If a server isn't doing what you expect, this is where you'll see it."
    ),
    "picker": (
        "Now, the terminal interface. Run gorun with the Gorunfile again, and the picker opens.\n"
        "Along the top are the tabs: Servers, Tasks, Bundles, Groups and History. "
        "You can jump to any of them with the number in front of its name.\n"
        "On the Servers tab, the servers are on the left, and the tasks of the server under the cursor are on the right. "
        "And the help line at the bottom always shows the keys you need."
    ),
    "select": (
        "Move around with j and k, or the arrow keys. Space selects a server. "
        "Open the Groups row, and selecting a group, like web, selects all of its servers. Tags work the same way.\n"
        "Press slash to filter. The match is fuzzy, so typing web narrows the list to the web servers. "
        "Enter keeps the filter, and escape clears it. And alt S selects everything that's showing."
    ),
    "tasks": (
        "The right-hand pane shows the tasks for the server under the cursor. "
        "Press tab to move into it, and space unticks a task, to leave it out of this run. "
        "Here I've unticked engine X, and the tasks that need it, site and smoke, are unticked too. "
        "The server row now says one of four tasks.\n"
        "And press the right arrow, or l, on a server to see its details: its address, user, port, key, groups and tags."
    ),
    "tabs": (
        "The other tabs let you look around. Tasks shows the dependency tree, with each task's description, command and settings.\n"
        "On Bundles, you can check a bundle with space. Then go back to Servers and pick the hosts, "
        "and that bundle runs instead of each server's own list.\n"
        "And Groups shows every group, and the servers in it."
    ),
    "run": (
        "When you're happy, press enter. Every server runs at the same time, each with its own progress bar, "
        "and the task it's on right now. Use j and k to move between servers, and enter to show a server's tasks. "
        "A real install takes longer, so I've sped this up.\n"
        "Press v for the verbose view, which lists every task on every server. "
        "Enter on a task opens its details and output, like the disk usage here."
    ),
    "fail": (
        "If a task fails, the tasks that depend on it are skipped, and the other servers carry on. "
        "gorun shows a red box with the reason, the exit code, and the end of the error output.\n"
        "Here, the smoke test on web three couldn't reach the web server. "
        "And because gorun exits with a non-zero status, your scripts and CI know that something went wrong."
    ),
    "history": (
        "Every run is saved. Open the History tab, or run gorun dash history, and you'll see your past runs, "
        "whether they passed, how long they took, and who ran them.\n"
        "Press enter to open one, expand a server to see its tasks, and enter on a task to see exactly what ran, "
        "and its exit code. When something fails in the middle of the night, this is where you find out why."
    ),
    "scripts": (
        "Sometimes you don't want a picker at all, like in scripts and CI. Dash dash run skips the picker, "
        "and runs everything selected. You can aim it with filters: a server, with a glob like web star, a tag, a group, or a task or bundle. "
        "Add dash dash plain for simple text output.\n"
        "One word of warning. Dash dash task runs that task on every selected server, even ones that don't normally run it. "
        "So aim it with a server, a tag or a group."
    ),
    "cheat": (
        "Here's a cheat sheet of the keys we used. Pause the video if you want to copy them down. "
        "And inside gorun, the question mark key shows every shortcut."
    ),
    "outro": (
        "So that's the basics: a Gorunfile with tasks, servers, groups, bundles and tags, and the terminal interface to pick and run them.\n"
        "In the next video, we'll make a deployment reliable, with checks that skip work that's already done, "
        "secrets that stay out of your file, and templates that give every server its own settings.\n"
        "Thanks so much for watching, and I'll see you there!"
    ),
}

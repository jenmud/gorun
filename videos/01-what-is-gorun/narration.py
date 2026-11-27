"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Hey there, and welcome! In this first video, I'm going to show you what gorun is, and why you might want it. "
        "I'll also be upfront about when you shouldn't use it.\n"
        "Then we'll roll up our sleeves and set up five brand new Ubuntu servers, with Docker and a little web page, "
        "all from one file."
    ),
    "voice": (
        "Quick note before we dive in. The voice you're hearing is synthetic. I know that can feel a bit soulless, "
        "so here's why we do it.\n"
        "gorun is a small open source project, and a generated voice lets us make tutorials quickly, "
        "and re-record them whenever something changes, so they don't go out of date.\n"
        "What's on screen is the real thing: the Gorunfile and the commands are all in the repository, "
        "so you can check them, and try them yourself. And if the voice isn't for you, "
        "there are captions, and a written tutorial that covers the same ground."
    ),
    "what": (
        "So, what is gorun? It's a single Go program that runs plain shell commands on a whole pile of servers, "
        "all at the same time, over SSH. And you get to watch it happen, live.\n"
        "You describe the work in a small TOML file, called a Gorunfile. Most people keep theirs in gorun's web dashboard, "
        "which stores every version in its database, and runs and schedules them from the browser. "
        "You can also keep one as a plain file, for the terminal and C I, which is what we'll do today. "
        "There's nothing to install on your servers. No agents, no daemons, no Python. "
        "If you can SSH into a machine, gorun can work on it."
    ),
    "why": (
        "Why might you like it? Well, think about the last time you had a handful of servers to set up, "
        "and all you wanted was to run the same few commands on each one.\n"
        "In gorun, a task is just a shell command. So when something goes wrong, you copy it, paste it into a terminal, "
        "and debug it by hand, just like you always do. There are no modules to learn.\n"
        "Every server runs at once, and if one fails, the others carry on. You pick the servers in a picker first, "
        "so nothing runs by accident. And every run is saved, so you can look back later and see exactly what happened."
    ),
    "ansible": (
        "Now, how does that compare with Ansible? Honestly, Ansible is a fantastic tool, and I'm not here to knock it. "
        "You describe the state you want, and it works out what needs to change. That's really powerful.\n"
        "But it's also the reason gorun exists. We wanted to be in control of what runs on the remote server, and how it runs, "
        "with no hidden abstractions in between. When something fails, we wanted a clear, actionable error, "
        "so you know exactly what the original error was. You can copy the command, paste it on the server, and run it by hand. "
        "And we were tired of keeping Python, modules and virtual environments up to date, just to run a few commands.\n"
        "So gorun trades that power for simplicity. If your thinking is, just run these commands over there, "
        "gorun stays out of your way. If it's, make sure this whole fleet looks like that, then Ansible is the better fit."
    ),
    "notgood": (
        "So let's be honest about what gorun is not good for. It runs commands. It doesn't enforce state, and it won't keep a fleet converged on its own.\n"
        "That means no modules, no facts, no automatic drift detection, no dynamic inventories, and no secrets vault.\n"
        "What it does have are checks. A task can carry a check, and if the check proves there's nothing to do, gorun skips the task. And the check option gives you a dry run: only the checks run, and gorun tells you which tasks would run. Just make sure your checks only look, and never change the machine.\n"
        "And if you manage hundreds of machines that have to stay converged on a declared state, reach for Ansible, Salt, Puppet or Nix. Plenty of people happily use both."
    ),
    "install": (
        "Installing is a one-liner. This script drops the binary into your local bin folder. "
        "You can also grab a build from the releases page, or build it yourself with go build.\n"
        "Once it's in, run gorun dash dump, and it writes out a starting Gorunfile, with every option explained in comments. "
        "It even runs as is, on your own machine, which makes it a lovely way to poke around."
    ),
    "concepts": (
        "Let's talk about the Gorunfile. It has just four kinds of section.\n"
        "First, tasks. A task is a named shell command, with options like sudo, a timeout, retries, a check, "
        "and dependencies on other tasks.\n"
        "Next, bundles. A bundle is just a name for a list of tasks, so you can say at-docker, "
        "instead of listing them one by one.\n"
        "Then server groups. These hold what a set of servers have in common, like which tasks they run, and how to log in.\n"
        "And finally, the servers themselves: a name, an address, and the groups they belong to. That's the whole model."
    ),
    "goal": (
        "Alright, let's build something real. I've got five brand new Ubuntu servers, "
        "and I can log in to each of them over SSH with a key.\n"
        "I want Docker on every one, my user in the docker group so I don't need sudo, "
        "and a Docker Compose file running Caddy, serving a small web page that shows the machine's resources."
    ),
    "task1": (
        "We'll start with a title, and our first task: installing Docker.\n"
        "The command runs Docker's official install script. But look at the check. It asks, is Docker already installed? If that command succeeds, gorun knows there's nothing to do, and skips the task.\n"
        "Sudo is true, so it runs as root. I've marked it safe to rerun, given it a fifteen minute timeout, and two retries, because freshly booted servers love to hold the package lock for a minute."
    ),
    "task2": (
        "Second task: add our user to the docker group. It has a check too, and a verify. The verify runs after the command, to confirm the change really took. If it fails, the task fails.\n"
        "When gorun uses sudo, the original user name is in the sudo user variable, so the same line works for everybody. And depends on says this has to wait for the Docker install. That's how you put things in order."
    ),
    "files": (
        "Next, we create a directory, and copy our Compose file over. A copy is simply a source on my laptop, and a destination on the server. Check copies tells gorun to compare the files first, so it only copies when they've changed.\n"
        "The Compose file runs two containers. Caddy, serving a folder on port eighty, and a tiny helper that rewrites an HTML page every five seconds, with the hostname, uptime, memory and disk.\n"
        "The last task runs docker compose up. Its check asks if both containers are already running, and its verify makes sure the web page answers."
    ),
    "wire": (
        "Now we tie it all together. A bundle called docker groups our two Docker tasks. "
        "A server group called ubuntu says what every server runs, and how to log in, so I only write that once.\n"
        "Then the five servers, each with a name, an address, and the group. "
        "Adding a sixth server later is just three more lines."
    ),
    "list": (
        "Before we run anything, let's check our work. Dash dash list servers shows every server, and the exact tasks it will run, in order. The dependencies are already sorted out for us."
    ),
    "dryrun": (
        "Now, before we touch a single server, let's do a dry run. Dash check runs only the checks. "
        "Nothing is copied, nothing is installed. gorun just tells us which tasks would run.\n"
        "These servers are brand new, so all twenty five tasks would run. That's exactly what we expect."
    ),
    "picker": (
        "Now for the fun part. Run gorun on the Gorunfile, and the picker opens. Nothing runs until I say so.\n"
        "I can select servers, groups or tags, and even leave out individual tasks. "
        "I'll select all five, and hit enter."
    ),
    "live": (
        "And off it goes! All five servers are working at the same time, each with its own progress bar, "
        "and the task it's on right now. If a task fails, the tasks that depend on it are skipped, and the other servers keep going.\n"
        "A real Docker install takes a minute or two, so I've sped this part up. And there we are. "
        "All twenty five tasks, done."
    ),
    "result": (
        "And here's the result. Browse to any of the servers, and there's Caddy, serving our little page, with the hostname, uptime, memory and disk. Five servers, one file, one command."
    ),
    "rerun": (
        "And remember those checks? Because of them, running it again is safe. Let's try it. "
        "Every check passes, so every task says unchanged, and the whole run is over in a blink.\n"
        "A dry run agrees: nothing would run. Fixed a typo, or added a sixth server? "
        "Just run it again, and only the new work happens."
    ),
    "outro": (
        "So that's gorun, in a nutshell. If you like plain commands, quick feedback, and not a lot of ceremony, "
        "I think you'll enjoy it.\n"
        "You'll find the install command and the full tutorial linked below. "
        "In the next video, we'll write a Gorunfile from scratch, one small step at a time, and take the terminal interface for a spin. "
        "And video five shows the web dashboard, where your Gorunfiles live in its database.\n"
        "Thanks so much for watching, and I'll see you there!"
    ),
}

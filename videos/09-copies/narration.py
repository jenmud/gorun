"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! Most deployments need to put files on servers: a config, a website, a release. "
        "Today, we're going to look at how gorun copies files, how it shows you the progress, "
        "and how it skips the files that are already there.\n"
        "This time the servers are real. They're three S S H servers, running in Docker on this machine, "
        "and every copy you'll see goes over S S H.\n"
        "Same synthetic voice as before."
    ),
    "basics": (
        "To copy files, add a copy table to a task. Copies run before the task's command.\n"
        "Here's the config task. The first copy takes a file from your machine, and because render is true, "
        "it's filled in as a template for each server. The server's name goes right into the file. "
        "The mode sets its permissions, and dst is where it goes on the server.\n"
        "The second copy has no source file at all. It's just inline content, also a template.\n"
        "And here's the result. Each server got its own config, and the mode really is six hundred."
    ),
    "rules": (
        "A few rules are worth knowing. Copies run as the login user, and not with sudo. "
        "So copy to somewhere you can write, like slash tmp, and move the file into place in the command.\n"
        "Files overwrite what's there, and the folders a copy needs are created for you. "
        "Owner and group are set with chown, so they need sudo on the task.\n"
        "Files are streamed, so a multi-gigabyte release is fine. And if you stop the run, with control C, the stop button, or a timeout, "
        "the copy stops part way."
    ),
    "folders": (
        "A source can be a whole folder, copied recursively, like S C P dash r. "
        "Here, the site task copies a folder with a style sheet, an image and a page.\n"
        "Files already on the server that aren't in your folder are left alone. "
        "Symbolic links are followed.\n"
        "Notice that the config task before it was unchanged, since its files hadn't changed. We'll come back to that."
    ),
    "progress": (
        "Now, a big one. This is a six hundred megabyte release, going to three servers at once. "
        "While it uploads, every server's line shows a bar, the percentage, the bytes sent, and the upload rate.\n"
        "With dash dash plain, for CI logs, gorun prints a line when the copy starts, about every ten percent, and when it's done. "
        "A copy is only done once the server has confirmed every byte. And the task's verify checks the checksum, so we know it arrived whole.\n"
        "The web dashboard shows the same progress, and so does the library, as an event."
    ),
    "checks": (
        "Now run it again. With check copies set to true, gorun compares each file with the one already on the server. "
        "The server hashes its own copy, so only the hashes cross the network, not the file. "
        "Everything matches, so all nine tasks are unchanged, and the six hundred megabytes isn't sent again.\n"
        "Now, I'll change one line of the style sheet, and ask with dash dash check. It tells us exactly what changed: one of three files, css slash app dot css. "
        "And a real run sends only that, and leaves the release alone.\n"
        "One more thing. If a server can't compare, for example when it has no md5 tool, gorun just copies anyway. Copying again is always safe."
    ),
    "library": (
        "If you're building on the Go library, you get the same progress. "
        "Config dot on event receives a copy progress event, with the file, the bytes, the percentage and the rate. "
        "And copy files context copies a task's files to a server from your own code, and stops if you cancel the context."
    ),
    "outro": (
        "So that's copying files. A copy table before the command. Templates and inline content. Whole folders. "
        "A live progress bar, and check copies to send only what changed.\n"
        "The written docs have every option, and they're linked below.\n"
        "Thanks so much for watching, and happy deploying!"
    ),
}

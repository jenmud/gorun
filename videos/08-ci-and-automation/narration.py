"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! So far, you've run gorun yourself, from the terminal or a browser. Today, we're going to let a machine do it: "
        "a CI job, a script, or cron.\n"
        "We'll run without the picker, fail a pipeline properly, aim a run at the right servers, and check before we change anything. "
        "Then we'll handle secrets, see how gorun records who ran what, and look at a GitHub Actions workflow.\n"
        "Same synthetic voice as before. The servers in this demo are stand-ins on one machine that only sleep, "
        "but the commands and the output are the real thing."
    ),
    "nopicker": (
        "By default, gorun opens the picker, and nothing runs until you choose it. A CI job has no terminal, so there's nobody to choose. "
        "If you run gorun like that, it stops with an error, rather than run something you didn't ask for.\n"
        "The fix is dash dash run, or just dash r, which skips the picker. Add dash dash plain for simple text output, "
        "and that's also what you get automatically when the output isn't a terminal.\n"
        "You get the plan, one line for every task as it finishes, and a summary line at the end. "
        "It's always the last line, so it's easy to read and to grep."
    ),
    "failing": (
        "Now, what happens when something fails? Here, the smoke test on prod two gets a five oh three. "
        "gorun shows the task, the exit code, and the end of the error output, then carries on with the other servers.\n"
        "The summary says one failed, and gorun exits with status one. That's all a pipeline needs: the job goes red, "
        "and the log says exactly what happened.\n"
        "A run that passes exits zero. And if someone presses control C twice, gorun quits at once with one thirty."
    ),
    "aim": (
        "In a pipeline, you almost always want to aim a run. Dash dash server picks servers by name or glob. "
        "Dash dash tag and dash dash group pick by label. Dash dash task runs one task, or a bundle, "
        "and exclude server leaves some out. Dash dash parallel limits how many servers run at once.\n"
        "To see what a command would do, add dash dash list servers with the same filters. Here are the two prod servers, and their tasks, in order. "
        "And here's the run for just staging. The filters make a job's intent obvious, right there in the pipeline file."
    ),
    "check": (
        "Before you change anything, ask what would change. Dash dash check runs only the checks, and says which tasks would run. "
        "Nothing is copied, installed or changed.\n"
        "Here, twelve would run. After a real run, the migrate task's check finds nothing to do, so it's unchanged, and nine would run.\n"
        "One thing to know. A check run exits zero even when tasks would run, so a pipeline doesn't fail on it. Read the last line, or use it as a preview step, "
        "so a person can see what a deployment is about to do. Just make sure your checks only look, and never change anything."
    ),
    "secrets": (
        "Secrets. CI keeps them for you, and hands them to a job as environment variables. "
        "In the Gorunfile, write a dollar sign and the variable's name, and gorun fills it in from its own environment.\n"
        "The deploy task here reads its token that way. The token itself never appears in the file.\n"
        "And it stays out of everything else. gorun sends the value to the task over standard input, "
        "so it doesn't show up in the logs, the history, the events, or the process list. "
        "Here I searched the log file for the token, and found it zero times."
    ),
    "origin": (
        "Once pipelines run things, you want to know which one ran what. gorun records that with every run.\n"
        "It recognises GitHub Actions, GitLab, Jenkins, Buildkite and CircleCI, and any other system that sets the C I variable. "
        "To show you what that looks like, I've set the variables GitHub Actions sets, by hand. Nothing here ran on GitHub.\n"
        "In the history, the run is marked with the C I system and who started it. Open it, and under who and where, "
        "there's the system, the user, a link to the job when the system provides one, the machine, the command, and the version.\n"
        "And you can add your own labels with G O R U N labels, like a change ticket number. "
        "They show up here, on the run's traces, and on its NATS events too."
    ),
    "workflow": (
        "Here's what a GitHub Actions workflow could look like. It's an example for you to adapt, and I haven't run it on GitHub.\n"
        "Check out the repository, and install gorun with the install script. Add its folder to the path.\n"
        "Then a dry run, with dash dash check, aimed at prod, so the log shows what would change.\n"
        "Then the deploy. The token comes from a repository secret, and the ticket comes from the workflow's input, as a label. "
        "Your job also needs a way to reach the servers: an S S H key kept as a secret, or a runner inside your network."
    ),
    "cron": (
        "No CI? You can run gorun dash r from cron, and the history still records every run.\n"
        "Or keep gorun dash serve running, and put your jobs in the Gorunfile as schedules. "
        "That gives you pause and skip, and a dashboard of every run, as we saw in video four."
    ),
    "outro": (
        "So that's gorun in automation. Skip the picker with dash dash run. Let the exit code speak. Aim with filters, and preview with dash dash check. "
        "Keep secrets in the environment, and let gorun record who ran what.\n"
        "The written docs cover every option, and they're linked below.\n"
        "Thanks so much for watching, and happy deploying!"
    ),
}

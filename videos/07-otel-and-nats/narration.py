"""Spoken script, one entry per scene id in build.py. Paragraph breaks ("\n") become longer pauses."""

NARRATION = {
    "intro": (
        "Welcome back! So far, we've watched gorun from its own screens. Today, we're going to watch it from the outside.\n"
        "First, OpenTelemetry: sending gorun's traces and metrics to Grafana. "
        "Then NATS: the message bus gorun publishes every event on, and how to start one embedded, or join one that already exists.\n"
        "Same synthetic voice as before. The servers in this demo are stand-ins on one machine that only sleep, "
        "but Grafana, NATS and everything you'll see are the real thing."
    ),
    "otelwhat": (
        "OpenTelemetry is a standard way for programs to report what they're doing. "
        "The instrumented build of gorun, which is what the install script gives you, supports it. "
        "The standard build doesn't.\n"
        "It's off until you point it somewhere. Set O T E L exporter O T L P endpoint to your collector, and gorun sends traces and metrics. "
        "G O R U N name becomes the service name.\n"
        "One tip: use this with gorun dash serve, or a program that stays running. "
        "A one-shot command can exit before the last batch of spans has been sent."
    ),
    "traces": (
        "Here's Grafana, with a collector, Tempo, Prometheus and Loki running in one container. "
        "In Tempo, each gorun run is one trace. Three runs from the web dashboard.\n"
        "Open one, and the shape of the run is right there. The top span is the whole run. Under it, one span for each server, "
        "and under those, one span for each task. You can see which server was slow, and which task took the time.\n"
        "Click the run span, and the attributes tell you who ran it, and where. "
        "The trigger is web, here, and it could also be the command line, a schedule, or a library. "
        "You also get the run id, the version, the Gorunfile, and the totals, including how many tasks failed."
    ),
    "metrics": (
        "gorun also sends metrics. There's a counter of task executions, labelled by server, task and status, "
        "a histogram of task durations, and a counter of skipped tasks.\n"
        "Here are executions in Prometheus. Web three has an error, and every other server is fine. "
        "And here's the ninety fifth percentile of how long each task takes. The slow ones stand out straight away.\n"
        "These are the numbers you'd put on a dashboard, or alert on: a task that suddenly fails everywhere, or one that's getting slower."
    ),
    "logs": (
        "Logs come along too, with no extra setup. Every log line gorun writes is also sent to the collector, and each one carries the id of the trace and the span it belongs to.\n"
        "Here's Loki, filtered to errors. These are the failed smoke tests, with the exit code, the reason, and the error output. "
        "Copy the trace id from one of them, and you can open the exact run in Tempo. "
        "Traces, metrics and logs, all from the same runs."
    ),
    "natswhat": (
        "Now, NATS. It's a small, fast message bus. Programs publish messages on a subject, and anyone listening on that subject receives them.\n"
        "gorun publishes every event of a run on the subject gorun dot events, as JSON: when a run starts, each server's plan, "
        "every change in a task's state, and when the run finishes.\n"
        "That's how the web dashboard's live view works. And you can listen too. "
        "This short Go program subscribes to gorun dot events and prints each one. Here it is, while a run goes by. "
        "Notice that a task's state is a number: two means done, and three means failed."
    ),
    "natsembedded": (
        "Where does the NATS server come from? When you start gorun dash serve, and nothing is listening on port four two two two, "
        "gorun starts a NATS server of its own, inside the same process. You don't install anything.\n"
        "The header shows how it's connected. Here it says embedded. "
        "And a run you start from a terminal on the same machine finds that server too, so it shows up on the dashboard, live, just like a run from the web page.\n"
        "To let other machines publish to it, give the embedded server a config file, and set G O R U N N A T S server config. "
        "Here it listens on every interface. There's no login, so keep it on a trusted network. "
        "And remember, the embedded server stops when gorun dash serve stops."
    ),
    "natsexternal": (
        "The other way is to join a NATS server that's already running. Maybe you run one for your whole team.\n"
        "Start it any way you like. Here it's the official Docker image. Then set G O R U N NATS client U R L to its address, "
        "and gorun uses it, and doesn't start its own. The header now says external.\n"
        "Point every machine's gorun at the same address, and one dashboard shows runs from all of them. "
        "A few other settings are there too: the client name, reconnect behaviour, and a timeout. "
        "And G O R U N skip NATS turns it all off, for when you don't want it."
    ),
    "outro": (
        "So that's gorun from the outside. OpenTelemetry gives you traces, metrics and logs in Grafana. "
        "And NATS gives you every event as it happens, from an embedded server, or one you already run.\n"
        "The written tutorial, with every option, is linked below.\n"
        "Thanks so much for watching, and happy deploying!"
    ),
}

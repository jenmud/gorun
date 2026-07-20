
**CAUTION:** This is still under development and does not work yet!!

# gorun
Gorun is a deployment orchestrator that executes remote server commands through SSH using a simple declarative configuration file.

## What It Does

* Defines deployment tasks with commands that can be linked together
* Automatically orders and runs tasks in the correct sequence
* Manages SSH connections and authentication
* Loads environment variables from config or .env files
* Runs deployments without requiring SSH installed

## Configuration

Configuration is done via environment variables. See `.env` for supported environment variable options.

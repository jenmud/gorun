Feature: Tasks run in dependency order
  A task runs after the tasks it depends on. When one fails, only the tasks that
  depend on it are skipped; the rest of the run carries on.

  Scenario: a failed task skips only the tasks that depend on it
    Given a Gorunfile:
      """
      [tasks.setup]
      cmd = "exit 3"
      local = true

      [tasks.site]
      cmd = "echo site"
      local = true
      depends_on = ["setup"]

      [tasks.disk]
      cmd = "echo disk"
      local = true
      """
    When I run every task
    Then "setup" is "failed"
    And "site" is "skipped"
    And "disk" is "done"
    And the run fails

  Scenario: a skip carries through a chain of dependencies
    Given a Gorunfile:
      """
      [tasks.setup]
      cmd = "exit 3"
      local = true

      [tasks.build]
      cmd = "echo build"
      local = true
      depends_on = ["setup"]

      [tasks.deploy]
      cmd = "echo deploy"
      local = true
      depends_on = ["build"]
      """
    When I run every task
    Then "setup" is "failed"
    And "build" is "skipped"
    And "deploy" is "skipped"
    And the run fails

Feature: A dry run changes nothing
  A check-only run runs only the checks. A task that would change something is
  reported as would run, and its command never runs.

  Scenario: a dry run reports what would run without running it
    Given a Gorunfile:
      """
      [tasks.install]
      check = "false"
      cmd = "exit 1"
      local = true

      [tasks.disk]
      check = "true"
      cmd = "exit 1"
      local = true
      """
    When I dry run every task
    Then "install" is "would run"
    And "disk" is "unchanged"
    And the run succeeds

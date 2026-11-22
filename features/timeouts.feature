Feature: A task cannot hang the run
  A task that runs past its timeout is stopped and fails. The run reports the
  failure instead of waiting for it.

  Scenario: a task that runs past its timeout fails
    Given a Gorunfile:
      """
      [tasks.slow]
      cmd = "sleep 5"
      timeout = "200ms"
      local = true

      [tasks.quick]
      cmd = "echo quick"
      local = true
      """
    When I run every task
    Then "slow" is "failed"
    And "quick" is "done"
    And the run fails

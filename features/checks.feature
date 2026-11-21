Feature: Checks skip work that is already done
  A task's check runs first. When the check says there is nothing to do, the
  task's command does not run and the task is unchanged.

  Scenario: a passing check means the command does not run
    Given a Gorunfile:
      """
      [tasks.install]
      check = "true"
      cmd = "exit 1"
      local = true
      """
    When I run every task
    Then "install" is "unchanged"
    And the run succeeds

  Scenario: a failing check runs the command
    Given a Gorunfile:
      """
      [tasks.install]
      check = "false"
      cmd = "echo installed"
      local = true
      """
    When I run every task
    Then "install" is "done"
    And the run succeeds

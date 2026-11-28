Feature: A verify confirms the change took
  A task's verify runs after its command. If the verify fails, the task fails,
  even though its command succeeded.

  Scenario: a passing verify leaves the task done
    Given a Gorunfile:
      """
      [tasks.install]
      cmd = "echo installed"
      verify = "true"
      local = true
      """
    When I run every task
    Then "install" is "done"
    And the run succeeds

  Scenario: a failing verify fails the task
    Given a Gorunfile:
      """
      [tasks.install]
      cmd = "echo installed"
      verify = "false"
      local = true
      """
    When I run every task
    Then "install" is "failed"
    And the run fails

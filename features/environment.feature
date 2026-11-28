Feature: Environment variables reach the command
  A task's envvars are set for its command, so secrets and settings can be
  passed in without writing them into the command itself.

  Scenario: envvars are set for the command
    Given a Gorunfile:
      """
      [tasks.greet]
      cmd = "test \"$GREETING\" = hello"
      envvars = { GREETING = "hello" }
      local = true
      """
    When I run every task
    Then "greet" is "done"
    And the run succeeds

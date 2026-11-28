package gorun_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cucumber/godog"

	"github.com/jenmud/gorun"
)

// TestFeatures runs the behaviour specs in features/. Each scenario is a plain-language rule of
// gorun, run against the library on this machine.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "gorun",
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("feature specs failed")
	}
}

// scenario holds one scenario's Gorunfile, what the run reported, and how it ended.
type scenario struct {
	cfg    gorun.Config
	mu     sync.Mutex
	states map[string]gorun.TaskState
	runErr error
}

func initScenario(ctx *godog.ScenarioContext) {
	s := &scenario{states: map[string]gorun.TaskState{}}
	ctx.Step(`^a Gorunfile:$`, s.aGorunfile)
	ctx.Step(`^I run every task$`, s.runEveryTask)
	ctx.Step(`^I dry run every task$`, s.dryRunEveryTask)
	ctx.Step(`^"([^"]*)" is "([^"]*)"$`, s.taskIs)
	ctx.Step(`^the run fails$`, s.runFails)
	ctx.Step(`^the run succeeds$`, s.runSucceeds)
}

func (s *scenario) aGorunfile(doc *godog.DocString) error {
	return s.cfg.Load(strings.NewReader(doc.Content))
}

func (s *scenario) runEveryTask() error {
	return s.run()
}

func (s *scenario) dryRunEveryTask() error {
	s.cfg.CheckOnly = true
	return s.run()
}

// run executes every task on the local machine and records each task's last reported state.
func (s *scenario) run() error {
	s.cfg.OnEvent = func(event any) {
		ev, ok := event.(gorun.TaskEvent)
		if !ok {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.states[ev.Task] = ev.State
	}
	s.runErr = s.cfg.RunLocal(context.Background())
	return nil
}

// taskIs checks the last reported state of a task against the name the spec gives it.
func (s *scenario) taskIs(task, want string) error {
	s.mu.Lock()
	got, ok := s.states[task]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("task %q was never reported", task)
	}
	if got.String() != want {
		return fmt.Errorf("task %q is %q, want %q", task, got, want)
	}
	return nil
}

func (s *scenario) runFails() error {
	if s.runErr == nil {
		return errors.New("the run succeeded, want it to fail")
	}
	return nil
}

func (s *scenario) runSucceeds() error {
	if s.runErr != nil {
		return fmt.Errorf("the run failed: %w", s.runErr)
	}
	return nil
}

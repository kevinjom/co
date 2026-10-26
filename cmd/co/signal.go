package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

type signalSession struct {
	ctx  context.Context
	stop context.CancelFunc
	sigs chan os.Signal
}

func startSignals(parent context.Context) *signalSession {
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	return &signalSession{
		ctx:  ctx,
		stop: stop,
		sigs: sigs,
	}
}

func (s *signalSession) close() {
	signal.Stop(s.sigs)
	s.stop()
}

func (s *signalSession) err() error {
	if s.ctx.Err() == nil {
		return nil
	}

	select {
	case sig := <-s.sigs:
		return exitForSignal(sig)
	default:
		return &exitError{Code: 130, Err: errors.New("interrupted")}
	}
}

func exitForSignal(sig os.Signal) error {
	switch sig {
	case os.Interrupt:
		return &exitError{Code: 130, Err: errors.New("interrupted")}
	case syscall.SIGTERM:
		return &exitError{Code: 143, Err: errors.New("terminated")}
	default:
		return &exitError{Code: 1, Err: errors.New(sig.String())}
	}
}

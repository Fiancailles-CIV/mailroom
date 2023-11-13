package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nyaruka/mailroom/runtime"
	"github.com/nyaruka/redisx"
)

// Function is the function that will be called on our schedule
type Function func(context.Context, *runtime.Runtime) (map[string]any, error)

// Start calls the passed in function every interval, making sure it acquires a
// lock so that only one process is running at once. Note that across processes
// crons may be called more often than duration as there is no inter-process
// coordination of cron fires. (this might be a worthy addition)
func Start(rt *runtime.Runtime, wg *sync.WaitGroup, name string, interval time.Duration, allInstances bool, cronFunc Function, timeout time.Duration, quit chan bool) {
	wg.Add(1) // add ourselves to the wait group

	lockName := fmt.Sprintf("lock:%s_lock", name) // for historical reasons...

	// for jobs that run on all instances, the lock key is specific to this instance
	if allInstances {
		lockName = fmt.Sprintf("%s:%s", lockName, rt.Config.InstanceName)
	}

	locker := redisx.NewLocker(lockName, time.Minute*5)

	wait := time.Duration(0)
	lastFire := time.Now()

	log := slog.With("cron", name)

	go func() {
		defer func() {
			log.Info("cron exiting")
			wg.Done()
		}()

		for {
			select {
			case <-quit:
				// we are exiting, return so our goroutine can exit
				return

			case <-time.After(wait):
				lastFire = time.Now()

				// try to get lock but don't retry - if lock is taken then task is still running or running on another instance
				lock, err := locker.Grab(rt.RP, 0)
				if err != nil {
					break
				}

				if lock == "" {
					log.Debug("lock already present, sleeping")
					break
				}

				// ok, got the lock, run our cron function
				start := time.Now()
				res, err := fireCron(rt, name, cronFunc)
				if err != nil {
					log.Error("error while running cron", "error", err)
				}
				elapsed := time.Since(start)

				// release our lock
				err = locker.Release(rt.RP, lock)
				if err != nil {
					log.Error("error releasing lock", "error", err)
				}

				logArgs := make([]any, 0, len(res)*2+2)
				for k, v := range res {
					logArgs = append(logArgs, k, v)
				}
				logArgs = append(logArgs, "elapsed", elapsed)

				// if cron too longer than a minute, log as error
				if elapsed > time.Minute {
					log.With(logArgs...).Error("cron took too long")
				} else {
					log.With(logArgs...).Info("cron completed")
				}
			}

			// calculate our next fire time
			nextFire := NextFire(lastFire, interval)
			wait = time.Until(nextFire)
			if wait < time.Duration(0) {
				wait = time.Duration(0)
			}
		}
	}()
}

// fireCron is just a wrapper around the cron function we will call for the purposes of
// catching and logging panics
func fireCron(rt *runtime.Runtime, name string, cronFunc Function) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	defer func() {
		// catch any panics and recover
		panicLog := recover()
		if panicLog != nil {
			slog.Error(fmt.Sprintf("panic running cron: %s", panicLog), "cron", name)
		}
	}()

	return cronFunc(ctx, rt)
}

// NextFire returns the next time we should fire based on the passed in time and interval
func NextFire(last time.Time, interval time.Duration) time.Time {
	if interval >= time.Second && interval < time.Minute {
		normalizedInterval := interval - ((time.Duration(last.Second()) * time.Second) % interval)
		return last.Add(normalizedInterval)
	} else if interval == time.Minute {
		seconds := time.Duration(60-last.Second()) + 1
		return last.Add(seconds * time.Second)
	} else {
		// no special treatment for other things
		return last.Add(interval)
	}
}

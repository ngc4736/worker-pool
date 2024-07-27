package workerpool

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"testing"
	"time"
)

func getTestLogger(l zapcore.Level) logr.Logger {
	encoderCfg := zap.NewDevelopmentEncoderConfig()
	encoderCfg.TimeKey = ""
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		zap.LevelEnablerFunc(func(level zapcore.Level) bool {
			return level == l
		}),
	)

	zapLogger := zap.New(core)
	defer zapLogger.Sync()

	return zapr.NewLogger(zapLogger).V(int(l))
}

func Test_New(t *testing.T) {
	tests := map[string]struct {
		opts                      []Opt[string]
		expectedMaxWorkerCount    uint32
		expectedMinWorkerCount    uint32
		expectedLoggerVerbosity   int
		expectedIdleWorkerTimeout time.Duration
		expectedError             error
	}{
		"should create worker pool with default params": {
			opts:                      []Opt[string]{},
			expectedLoggerVerbosity:   defaultLogger().GetV(),
			expectedMaxWorkerCount:    DefaultMaxWorkersCount,
			expectedIdleWorkerTimeout: DefaultIdleWorkerTimeout,
			expectedError:             nil,
		},
		"should create worker pool with custom params": {
			opts: []Opt[string]{
				WithLogger[string](getTestLogger(zapcore.ErrorLevel)),
				WithMaxWorkerCount[string](5),
				WithIdleWorkerTimeout[string](time.Second),
			},
			expectedLoggerVerbosity:   getTestLogger(zapcore.ErrorLevel).GetV(),
			expectedMaxWorkerCount:    5,
			expectedMinWorkerCount:    2,
			expectedIdleWorkerTimeout: time.Second,
			expectedError:             nil,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			wp := New[string](test.opts...)
			assert.Equal(t, test.expectedLoggerVerbosity, wp.Logger().GetV())
			assert.Equal(t, test.expectedMaxWorkerCount, wp.MaxWorkerCount())
			assert.Equal(t, test.expectedIdleWorkerTimeout, wp.idleWorkerTimeout)
		})
	}
}

func Test_worker(t *testing.T) {
	defer goleak.VerifyNone(t)

	testResult := "test result"
	testErr := fmt.Errorf("test err")

	tests := map[string]struct {
		task           func() (string, error)
		expectedResult string
		expectedError  error
	}{
		"should return result": {
			task: func() (string, error) {
				return testResult, nil
			},
			expectedResult: testResult,
			expectedError:  nil,
		},
		"should return error": {
			task: func() (string, error) {
				return "", testErr
			},
			expectedResult: "",
			expectedError:  testErr,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			wp := New[string]()

			wp.worker(0)

			wp.tasksToExecute <- test.task

			var result string
			var err error
			select {
			case result = <-wp.results:
			case err = <-wp.errors:
			}

			assert.Equal(t, test.expectedResult, result)
			assert.Equal(t, test.expectedError, err)

			close(wp.stopSignal)
		})
	}
}

func Test_WorkerPool_IdleWorkerTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := map[string]struct {
		workerIdleTimeout   time.Duration
		delay               time.Duration
		expectedWorkerCount uint32
	}{
		"should wait": {
			workerIdleTimeout:   time.Second * 2,
			delay:               time.Second,
			expectedWorkerCount: 1,
		},
		"should timeout": {
			workerIdleTimeout:   time.Second,
			delay:               time.Second * 2,
			expectedWorkerCount: 0,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			wp := New[string](
				WithIdleWorkerTimeout[string](test.workerIdleTimeout),
			)
			results, _ := wp.Start()

			task := func() (string, error) { return "", nil }

			wp.Submit(task)

			<-results
			<-time.After(test.delay)

			workerCount := wp.WorkerCount()

			assert.Equal(t, test.expectedWorkerCount, workerCount)
			wp.Stop()
		})
	}
}

func Test_WorkerPool_MultipleTasks(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		taskCount      int
		maxWorkerCount int
	}{
		{
			taskCount:      20,
			maxWorkerCount: 10,
		},
	}

	testLogger := getTestLogger(zapcore.ErrorLevel).WithName("test")

	for testCount, test := range tests {
		poolName := fmt.Sprintf("pool-%d", testCount)

		wp := New[string](WithLogger[string](getTestLogger(zap.ErrorLevel)))

		results, _ := wp.Start()

		for i := 1; i <= test.taskCount; i++ {
			task := func() (string, error) {
				time.Sleep(time.Second)
				return fmt.Sprintf("result of task-%d", i), nil
			}
			testLogger.Info("submit task")
			wp.Submit(task)
		}

		for i := 0; i < test.taskCount; i++ {
			testLogger.Info("attempting to receive result...")
			result, ok := <-results
			if !ok {
				testLogger.Info("results chan is closed")
				break
			}
			testLogger.Info("result received", "result", result)
		}

		testLogger.Info("stop worker pool")
		wp.Stop()

		testLogger.Info("test completed", "workerPool", poolName)
		fmt.Println()
	}
}

//func TestWorkerPool_SucceedOrError(t *testing.T) {
//	defer goleak.VerifyNone(t)
//
//	tests := []struct {
//		taskCount      int
//		maxWorkerCount int
//	}{
//		{
//			taskCount:      20,
//			maxWorkerCount: 10,
//		},
//	}
//
//	testLogger := getTestLogger(zapcore.InfoLevel).WithName("test")
//
//	for testCount, test := range tests {
//		poolName := fmt.Sprintf("pool-%d", testCount)
//
//		var wp TestPool[string]
//		wp = New[string](test.maxWorkerCount, getTestLogger(zapcore.ErrorLevel).WithName("pool"))
//
//		timer := getTimer()
//		results, errors := wp.Start()
//
//		for i := 1; i <= test.taskCount; i++ {
//			task := func() (string, error) {
//				time.Sleep(time.Second)
//
//				correct := (rand.Intn(10) % 2) == 0
//
//				if !correct {
//					return "", fmt.Errorf("failed task-%d", i)
//				}
//
//				return fmt.Sprintf("result of task-%d", i), nil
//			}
//			testLogger.Info("submit task", "id", i)
//			wp.Submit(task)
//		}
//
//		for i := 0; i < test.taskCount; i++ {
//			select {
//			case result, ok := <-results:
//				if !ok {
//					testLogger.Info("results chan is closed")
//					break
//				}
//				testLogger.Info("result received", "result", result)
//			case err, ok := <-errors:
//				if !ok {
//					testLogger.Info("errors chan is closed")
//					break
//				}
//				testLogger.Info("error received", "err", err)
//			}
//		}
//
//		testLogger.Info("stop worker pool")
//		wp.Stop()
//		duration := timer()
//
//		testLogger.Info("test completed", "workerPool", poolName, "duration", duration)
//		fmt.Println()
//	}
//}
//
//func getTimer() func() time.Duration {
//	start := time.Now()
//	return func() time.Duration {
//		return time.Since(start)
//	}
//}

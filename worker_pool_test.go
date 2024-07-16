package workerpool

import (
	"fmt"
	"github.com/go-logr/zapr"
	"go.uber.org/goleak"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"testing"
	"time"
)

type testTask struct {
	id    int
	delay time.Duration
}

func (t testTask) String() string {
	return fmt.Sprintf("task-%d", t.id)
}

func (t testTask) Execute() string {
	time.Sleep(t.delay)
	return fmt.Sprintf("result of task-%d", t.id)
}

func TestWorkerPool_TaskExecution(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		taskCount      int
		maxWorkerCount int
	}{
		{
			//TODO: livelock with this test input (3, 1)
			taskCount:      100,
			maxWorkerCount: 50,
		},
		//{
		//	taskCount:      5,
		//	maxWorkerCount: 2,
		//},
		//{
		//	taskCount:      2,
		//	maxWorkerCount: 5,
		//},
	}

	atom := zap.NewAtomicLevel()

	encoderCfg := zap.NewDevelopmentEncoderConfig()
	encoderCfg.TimeKey = ""
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))

	defer zapLogger.Sync()

	atom.SetLevel(zap.InfoLevel)

	logger := zapr.NewLogger(zapLogger)

	testLogger := logger.WithName("test")

	for i, test := range tests {
		poolName := fmt.Sprintf("pool-%d", i)
		wp := NewWorkerPool[string](test.maxWorkerCount, logger.WithName(poolName))

		timer := getTimer()
		results := wp.Start()

		for i := 1; i <= test.taskCount; i++ {
			task := testTask{
				id:    i,
				delay: time.Second,
			}
			testLogger.Info("submit task", "taskId", task)
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
		duration := timer()

		testLogger.Info("test completed", "workerPool", poolName, "duration", duration)
		fmt.Println()
	}
}

func getTimer() func() time.Duration {
	start := time.Now()
	return func() time.Duration {
		return time.Since(start)
	}
}

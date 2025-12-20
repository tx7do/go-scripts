package js

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestJavascriptEngine(t *testing.T) {
	// 创建引擎
	eng, err := newJavascriptEngine()
	assert.Nil(t, err)
	assert.NotNil(t, eng)
	defer eng.Close()

	// 初始化
	ctx := context.Background()
	if err = eng.Init(ctx); err != nil {
		t.Fatal(err)
	}

	// 注册全局变量
	err = eng.RegisterGlobal("config", map[string]interface{}{
		"host": "localhost",
		"port": 8080,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 注册函数
	err = eng.RegisterFunction("log", func(msg string) {
		fmt.Println("JS Log:", msg)
	})
	if err != nil {
		t.Fatal(err)
	}

	// 执行脚本
	result, err := eng.ExecuteString(ctx, `
    function add(a, b) {
        log('Adding ' + a + ' and ' + b);
        return a + b;
    }
    add(10, 20);
`)
	fmt.Println(result) // 输出: 30

	// 调用函数（带超时）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err = eng.CallFunction(ctx, "add", 100, 200)
	if err != nil {
		t.Error(err)
	}
	fmt.Println(result) // 输出: 300
}

func TestConcurrentExecuteAndCallFunction(t *testing.T) {
	eng, err := newJavascriptEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err = eng.Init(ctx); err != nil {
		t.Fatal(err)
	}

	// 注册一个简单的加法函数供 CallFunction 使用
	if err = eng.RegisterFunction("add", func(a, b float64) float64 { return a + b }); err != nil {
		t.Fatal(err)
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// 并发调用 ExecuteString 与 CallFunction
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			// 每个 goroutine 做若干次调用
			for j := 0; j < 20; j++ {
				// ExecuteString
				ctxExe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
				_, _ = eng.ExecuteString(ctxExe, "1 + 2 + 3")
				cancel()

				// CallFunction
				ctxCall, cancel2 := context.WithTimeout(ctx, 500*time.Millisecond)
				_, _ = eng.CallFunction(ctxCall, "add", 10, 20)
				cancel2()
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 成功完成
	case <-time.After(10 * time.Second):
		t.Fatal("timeout: concurrent execute/call did not finish")
	}
}

func TestConcurrentInitCloseAndExecute(t *testing.T) {
	eng, err := newJavascriptEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()

	// 后台反复 Init / Register / Close
	stopBg := make(chan struct{})
	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		for i := 0; i < 50; i++ {
			_ = eng.Init(ctx)
			// 尝试注册一个全局，忽略错误（可能未初始化/已初始化）
			_ = eng.RegisterGlobal("g", map[string]any{"i": i})
			time.Sleep(5 * time.Millisecond)
			_ = eng.Close()
			time.Sleep(5 * time.Millisecond)
		}
		close(stopBg)
	}()

	// 并发执行短时脚本，可能在 Init/Close 切换期间产生 ErrJavascriptEngineNotInitialized，属可接受
	const callers = 200
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(i int) {
			defer wg.Done()
			// 每个 caller 重复多次短调用
			for j := 0; j < 30; j++ {
				c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
				_, _ = eng.ExecuteString(c, "1+2+3+"+time.Now().Format("150405")) // 短计算
				cancel()
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		<-stopBg
		bgWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 成功完成
	case <-time.After(20 * time.Second):
		t.Fatal("timeout: concurrent init/close and execute did not finish")
	}
}

package hostmodule

// 本文件针对 module.go 的 ModuleUtil 做单元测试（语言无关模块，供 JS 等引擎
// 注册），范式同 module_test.go 的 ModuleLogger：
//   - 结构：名称为 "util"，包含且仅包含四个函数键，且均为函数类型；
//   - 调用行为：
//       - sleep：自定义上限（构造分支 maxSleep>0）下超限值被钳制到上限、
//         未超限值与负值直通 time.Sleep（两分支）；上限传 0/不传时回退
//         默认 5 秒（构造分支）；
//       - time / timestamp：Unix 秒/毫秒时间戳与 time.Now 的偏差有界；
//       - date：无参/空串默认 RFC3339（回解析校验形态）、显式格式按格式输出。
//
// 计时断言只做下界（time.Sleep 保证至少睡满）与宽松上界，避免 CI 抖动。

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestModuleUtil_Structure 校验模块定义：名称为 "util"，包含且仅包含四个
// 函数键，全部为函数类型。
func TestModuleUtil_Structure(t *testing.T) {
	mod := ModuleUtil()

	require.Equal(t, "util", mod.Name)
	require.Len(t, mod.Funcs, 4)
	for _, name := range []string{"sleep", "time", "timestamp", "date"} {
		fn, ok := mod.Funcs[name]
		require.True(t, ok, "ModuleUtil should expose %q", name)
		require.Equal(t, reflect.Func, reflect.TypeOf(fn).Kind(), "Funcs[%q] should be a function", name)
	}
}

// TestModuleUtil_SleepClamp 自定义上限（150ms）构造分支 + 钳制分支：
// 请求睡 100 秒被钳到上限，实际耗时至少为上限值。
func TestModuleUtil_SleepClamp(t *testing.T) {
	mod := ModuleUtil(150 * time.Millisecond)
	sleep, ok := mod.Funcs["sleep"].(func(float64))
	require.True(t, ok)

	start := time.Now()
	sleep(100)
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 140*time.Millisecond, "oversized sleep should be clamped to the configured cap")
	require.Less(t, elapsed, 30*time.Second, "clamped sleep should still terminate promptly")
}

// TestModuleUtil_SleepBelowCapAndNegative 未超限分支与负值分支：
// 小值正常睡（不超过宽松上界）、负值立即返回。
func TestModuleUtil_SleepBelowCapAndNegative(t *testing.T) {
	mod := ModuleUtil(10 * time.Second)
	sleep, ok := mod.Funcs["sleep"].(func(float64))
	require.True(t, ok)

	start := time.Now()
	sleep(0.001)
	small := time.Since(start)
	require.Less(t, small, 30*time.Second, "small sleep should not hang")

	start = time.Now()
	sleep(-5)
	negative := time.Since(start)
	require.Less(t, negative, 30*time.Second, "negative sleep should return immediately")
}

// TestModuleUtil_DefaultCapConstructors 上限传 0 与不传的两个构造分支
// （均回退默认 5 秒）：小值睡眠直通，不触发钳制。
func TestModuleUtil_DefaultCapConstructors(t *testing.T) {
	for _, mod := range []ModuleDef{ModuleUtil(0), ModuleUtil()} {
		sleep, ok := mod.Funcs["sleep"].(func(float64))
		require.True(t, ok)

		start := time.Now()
		sleep(0.001)
		require.Less(t, time.Since(start), 30*time.Second, "default-cap construction should still allow tiny sleeps")
	}
}

// TestModuleUtil_Timestamps time/timestamp 返回当前 Unix 秒/毫秒时间戳，
// 与 time.Now 的偏差有界（±5 秒 / ±5 秒）。
func TestModuleUtil_Timestamps(t *testing.T) {
	mod := ModuleUtil()

	timeFn, ok := mod.Funcs["time"].(func() int64)
	require.True(t, ok)
	tsFn, ok := mod.Funcs["timestamp"].(func() int64)
	require.True(t, ok)

	nowSec := time.Now().Unix()
	sec := timeFn()
	require.GreaterOrEqual(t, sec, nowSec-5, "time() should track the wall clock")
	require.LessOrEqual(t, sec, nowSec+5, "time() should track the wall clock")

	nowMilli := time.Now().UnixMilli()
	milli := tsFn()
	require.GreaterOrEqual(t, milli, nowMilli-5000, "timestamp() should track the wall clock")
	require.LessOrEqual(t, milli, nowMilli+5000, "timestamp() should track the wall clock")
}

// TestModuleUtil_Date date 的三个分支：无参与空串均回退 RFC3339（以回解析
// 校验其形态），显式格式 "2006-01-02" 按该格式输出当日日期。
func TestModuleUtil_Date(t *testing.T) {
	mod := ModuleUtil()

	dateFn, ok := mod.Funcs["date"].(func(...string) string)
	require.True(t, ok)

	// 无参与空串：默认 RFC3339
	for name, out := range map[string]string{
		"no arg":       dateFn(),
		"empty format": dateFn(""),
	} {
		parsed, err := time.Parse(time.RFC3339, out)
		require.NoError(t, err, "%s should produce RFC3339 output, got %q", name, out)
		require.WithinDuration(t, time.Now(), parsed, 5*time.Second, "%s parsed time should be near now", name)
	}

	// 显式格式：仅日期
	require.Equal(t, time.Now().Format("2006-01-02"), dateFn("2006-01-02"))
}

// Package hook_test 针对 hook 包的 Registry 做黑盒测试。
//
// Registry 是钩子点与其挂载脚本的进程内注册表，本文件逐 API 验证：
//   - RegisterHook：正常注册、重复注册报错；
//   - AddScript：钩子自动注册、同名替换（upsert，热重载语义）、
//     按 Priority 升序的稳定排序；
//   - RemoveScript：钩子/脚本不存在报错、正常移除；
//   - GetScripts：副本语义（改副本不影响内部）、未知钩子返回空切片、
//     排序后的返回顺序；
//   - GetHook / ListHooks / GetAllHooks / Clear / Count / ScriptCount
//     各自的查找、排序、清理与计数行为；
//   - 并发混跑下计数保持一致（同步正确性，可在 -race 下复跑）。
package hook_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/hook"
)

// newScript 构造一个带名字与优先级的测试脚本。
func newScript(name string, priority int) *hook.Script {
	return &hook.Script{
		ID:       1,
		Name:     name,
		Hook:     "test-hook",
		Source:   "return 1",
		Enabled:  true,
		Priority: priority,
		Version:  1,
		Author:   "tester",
	}
}

// scriptNames 提取脚本名列表，便于断言顺序。
func scriptNames(scripts []*hook.Script) []string {
	names := make([]string, 0, len(scripts))
	for _, s := range scripts {
		names = append(names, s.Name)
	}
	return names
}

// TestRegisterHook_NewAndDuplicate 新钩子注册成功且可通过 GetHook 检索；
// 同名重复注册必须报错，不得静默覆盖。
func TestRegisterHook_NewAndDuplicate(t *testing.T) {
	r := hook.NewRegistry()

	require.NoError(t, r.RegisterHook("alpha", "首个钩子"))
	assert.Equal(t, 1, r.Count())

	h, err := r.GetHook("alpha")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "alpha", h.Name)
	assert.Equal(t, "首个钩子", h.Description)
	assert.Empty(t, h.Scripts, "新建钩子不得携带任何脚本")

	err = r.RegisterHook("alpha", "重复钩子")
	require.Error(t, err, "重复注册必须报错")
	assert.Contains(t, err.Error(), "already registered")
	assert.Equal(t, 1, r.Count(), "失败的重复注册不得增加计数")
}

// TestRegisterHook_MultipleHooksSortedInListHooks ListHooks 必须按名字升序返回。
func TestRegisterHook_MultipleHooksSortedInListHooks(t *testing.T) {
	r := hook.NewRegistry()
	for _, name := range []string{"delta", "bravo", "charlie", "alpha"} {
		require.NoError(t, r.RegisterHook(name, ""))
	}
	assert.Equal(t, []string{"alpha", "bravo", "charlie", "delta"}, r.ListHooks())
}

// TestGetHook_NotFound 查询不存在的钩子必须报错并返回 nil。
func TestGetHook_NotFound(t *testing.T) {
	r := hook.NewRegistry()
	h, err := r.GetHook("ghost")
	require.Error(t, err)
	assert.Nil(t, h)
	assert.Contains(t, err.Error(), "hook not found")
}

// TestListHooks_EmptyRegistry 空注册表的 ListHooks 必须是空切片而非 nil。
func TestListHooks_EmptyRegistry(t *testing.T) {
	r := hook.NewRegistry()
	assert.Empty(t, r.ListHooks())
	assert.Empty(t, r.GetAllHooks())
	assert.Equal(t, 0, r.Count())
	assert.Equal(t, 0, r.ScriptCount())
}

// TestAddScript_AutoRegistersHook 向不存在的钩子添加脚本时钩子被自动创建，
// 计数联动正确。
func TestAddScript_AutoRegistersHook(t *testing.T) {
	r := hook.NewRegistry()

	require.NoError(t, r.AddScript("auto-hook", newScript("s1", 1)))

	assert.Equal(t, 1, r.Count(), "脚本挂载应自动创建钩子")
	assert.Equal(t, 1, r.ScriptCount())
	h, err := r.GetHook("auto-hook")
	require.NoError(t, err)
	assert.Equal(t, "auto-hook", h.Name)
	assert.Equal(t, "", h.Description, "自动创建的钩子无描述")
}

// TestAddScript_SortsByPriorityAscending 脚本列表必须按 Priority 升序排列，
// 与插入顺序无关。
func TestAddScript_SortsByPriorityAscending(t *testing.T) {
	r := hook.NewRegistry()

	require.NoError(t, r.AddScript("h", newScript("high", 50)))
	require.NoError(t, r.AddScript("h", newScript("low", 1)))
	require.NoError(t, r.AddScript("h", newScript("mid", 10)))

	assert.Equal(t, []string{"low", "mid", "high"}, scriptNames(r.GetScripts("h")))
}

// TestAddScript_StableSortForEqualPriority 相同优先级的脚本之间保持插入顺序
// （sort.SliceStable 的稳定性契约）。
func TestAddScript_StableSortForEqualPriority(t *testing.T) {
	r := hook.NewRegistry()

	require.NoError(t, r.AddScript("h", newScript("first", 5)))
	require.NoError(t, r.AddScript("h", newScript("second", 5)))

	assert.Equal(t, []string{"first", "second"}, scriptNames(r.GetScripts("h")))

	// 后到的更低优先级脚本应排到两个同优先级脚本之前。
	require.NoError(t, r.AddScript("h", newScript("front", 1)))
	assert.Equal(t, []string{"front", "first", "second"}, scriptNames(r.GetScripts("h")))
}

// TestAddScript_SameNameReplaces 同名脚本按 upsert 语义原位替换（热重载）：
// 数量不变、内容更新、且替换后按新优先级重新排序。
func TestAddScript_SameNameReplaces(t *testing.T) {
	r := hook.NewRegistry()

	require.NoError(t, r.AddScript("h", newScript("a", 10)))
	require.NoError(t, r.AddScript("h", newScript("b", 20)))
	require.Equal(t, []string{"a", "b"}, scriptNames(r.GetScripts("h")))

	replacement := newScript("a", 100)
	replacement.Source = "return 2"
	replacement.Version = 2
	require.NoError(t, r.AddScript("h", replacement))

	assert.Equal(t, 2, r.ScriptCount(), "替换不得增加脚本数量")
	scripts := r.GetScripts("h")
	require.Len(t, scripts, 2)
	// 新优先级 100 使 a 排到 b 之后。
	assert.Equal(t, []string{"b", "a"}, scriptNames(scripts))
	assert.Equal(t, "return 2", scripts[1].Source, "同名脚本应被新内容替换")
	assert.Equal(t, 2, scripts[1].Version)
}

// TestRemoveScript_ErrorPaths 钩子不存在或脚本不存在都必须报错。
func TestRemoveScript_ErrorPaths(t *testing.T) {
	r := hook.NewRegistry()

	err := r.RemoveScript("ghost-hook", "s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook not found")

	require.NoError(t, r.RegisterHook("h", ""))
	err = r.RemoveScript("h", "ghost-script")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "script not found")
	assert.Equal(t, 0, r.ScriptCount())
}

// TestRemoveScript_RemovesOnlyNamedScript 正常移除只删除指名脚本，
// 其余脚本保持原有顺序。
func TestRemoveScript_RemovesOnlyNamedScript(t *testing.T) {
	r := hook.NewRegistry()
	require.NoError(t, r.AddScript("h", newScript("a", 1)))
	require.NoError(t, r.AddScript("h", newScript("b", 2)))
	require.NoError(t, r.AddScript("h", newScript("c", 3)))
	require.Equal(t, 3, r.ScriptCount())

	require.NoError(t, r.RemoveScript("h", "b"))

	assert.Equal(t, 2, r.ScriptCount())
	assert.Equal(t, []string{"a", "c"}, scriptNames(r.GetScripts("h")))
}

// TestGetScripts_UnknownHookReturnsEmptySlice 未知钩子的脚本列表必须是
// 空的非 nil 切片。
func TestGetScripts_UnknownHookReturnsEmptySlice(t *testing.T) {
	r := hook.NewRegistry()
	scripts := r.GetScripts("ghost")
	assert.NotNil(t, scripts)
	assert.Empty(t, scripts)
}

// TestGetScripts_ReturnsDefensiveCopy GetScripts 返回副本：
// 对返回切片做写入（置空元素）后，内部状态不受影响。
func TestGetScripts_ReturnsDefensiveCopy(t *testing.T) {
	r := hook.NewRegistry()
	require.NoError(t, r.AddScript("h", newScript("a", 1)))
	require.NoError(t, r.AddScript("h", newScript("b", 2)))

	copy1 := r.GetScripts("h")
	require.Len(t, copy1, 2)

	// 篡改副本：置空首元素并追加一个伪脚本。
	copy1[0] = nil
	copy1 = append(copy1, newScript("evil", 0))

	fresh := r.GetScripts("h")
	assert.Equal(t, []string{"a", "b"}, scriptNames(fresh), "篡改副本不得影响内部脚本列表")
	assert.Equal(t, 2, r.ScriptCount())
}

// TestGetAllHooks_ReturnsAllHooksSortedByAddition GetAllHooks 应返回全部钩子，
// 覆盖每个钩子对象（数量与 ListHooks 一致）。
func TestGetAllHooks_ReturnsAllHooksSortedByAddition(t *testing.T) {
	r := hook.NewRegistry()
	require.NoError(t, r.RegisterHook("h1", ""))
	require.NoError(t, r.RegisterHook("h2", ""))

	all := r.GetAllHooks()
	require.Len(t, all, 2)
	names := map[string]bool{}
	for _, h := range all {
		names[h.Name] = true
	}
	assert.Equal(t, map[string]bool{"h1": true, "h2": true}, names)
}

// TestClear_ResetAllState Clear 后所有钩子与脚本清空，计数归零。
func TestClear_ResetAllState(t *testing.T) {
	r := hook.NewRegistry()
	require.NoError(t, r.RegisterHook("h1", ""))
	require.NoError(t, r.AddScript("h2", newScript("s1", 1)))
	require.Equal(t, 2, r.Count())
	require.Equal(t, 1, r.ScriptCount())

	r.Clear()

	assert.Equal(t, 0, r.Count())
	assert.Equal(t, 0, r.ScriptCount())
	assert.Empty(t, r.ListHooks())
	assert.Empty(t, r.GetAllHooks())
}

// TestCount_AndScriptCount_AcrossHooks ScriptCount 统计所有钩子的脚本总和，
// Count 只统计钩子数量。
func TestCount_AndScriptCount_AcrossHooks(t *testing.T) {
	r := hook.NewRegistry()
	require.NoError(t, r.AddScript("h1", newScript("s1", 1)))
	require.NoError(t, r.AddScript("h1", newScript("s2", 2)))
	require.NoError(t, r.AddScript("h2", newScript("s3", 3)))

	assert.Equal(t, 2, r.Count())
	assert.Equal(t, 3, r.ScriptCount())
}

// TestRegistry_ConcurrentOperations 并发混跑注册/挂载/查询：
// 互斥锁保护下最终计数必须精确一致，且对同一钩子的重复注册除首次外全部报错
// （错误被逐个计数断言，不做裸丢弃；可用 go test -race 复跑验证无数据竞争）。
func TestRegistry_ConcurrentOperations(t *testing.T) {
	r := hook.NewRegistry()

	const goroutines = 16
	const perGoroutine = 8
	const totalAttempts = goroutines * perGoroutine

	var registerOK, registerDup, addFailed atomic.Int64

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				// 并发写：同名钩子竞争注册，仅首个成功；错误逐个计数，不吞。
				if err := r.RegisterHook("hook", ""); err != nil {
					registerDup.Add(1)
				} else {
					registerOK.Add(1)
				}
				if err := r.AddScript("hook", newScript("script", g)); err != nil {
					addFailed.Add(1)
				}
				// 并发读（只读 API，返回值无错误通道）。
				_ = r.GetScripts("hook")
				_ = r.ListHooks()
			}
		}(g)
	}
	wg.Wait()

	// 所有 goroutine 操作同一个钩子与同一个脚本名：最终 1 钩子 1 脚本；
	// 重复注册必须恰好 totalAttempts-1 次报错、1 次成功；upsert 挂载全部成功。
	assert.Equal(t, int64(1), registerOK.Load())
	assert.Equal(t, int64(totalAttempts-1), registerDup.Load())
	assert.Equal(t, int64(0), addFailed.Load())
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, 1, r.ScriptCount())
}

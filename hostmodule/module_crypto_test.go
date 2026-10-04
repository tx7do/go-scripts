package hostmodule

// 本文件针对 module.go 的 ModuleCrypto 做单元测试（语言无关模块，供 JS 等
// 引擎注册），范式同 module_test.go 的 ModuleLogger：
//   - 结构：名称为 "crypto"，包含且仅包含六个函数键，且均为函数类型；
//   - 调用行为：encrypt/decrypt 往返（"enc:" 前缀、is_encrypted 判定）、
//     空串直返、非加密串透传、三类非法密文（坏 base64 / 短密文 / 认证失败）
//     的错误分支；
//   - encrypt_json/decrypt_json：对象/嵌套/数组三种 JSON 形态的加密往返、
//     非 JSON 与非法密文的错误分支、含 NaN 值的序列化失败分支、非加密 JSON
//     明文的透传解码分支；
//   - hash_sha256：标准测试向量（"test"、空串）。
//
// 加密依赖全局加密器：测试内以固定 32 字节 key 初始化（幂等，Once 保护）。

import (
	"encoding/base64"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-utils/crypto"
)

// initModuleCryptoTestEncryptor 初始化全局加密器（幂等；与既有 crypto_test.go
// 使用同一 key 与启用开关，保证包内测试状态一致）。
func initModuleCryptoTestEncryptor(t *testing.T) {
	t.Helper()
	require.NoError(t, crypto.InitGlobalEncryptor("test-encryption-key-32-bytes-long!", true))
}

// TestModuleCrypto_Structure 校验模块定义：名称为 "crypto"，包含且仅包含
// 六个函数键，全部为函数类型。
func TestModuleCrypto_Structure(t *testing.T) {
	mod := ModuleCrypto()

	require.Equal(t, "crypto", mod.Name)
	require.Len(t, mod.Funcs, 6)
	for _, name := range []string{"encrypt", "decrypt", "is_encrypted", "encrypt_json", "decrypt_json", "hash_sha256"} {
		fn, ok := mod.Funcs[name]
		require.True(t, ok, "ModuleCrypto should expose %q", name)
		require.Equal(t, reflect.Func, reflect.TypeOf(fn).Kind(), "Funcs[%q] should be a function", name)
	}
}

// TestModuleCrypto_EncryptDecryptRoundTrip 加密/解密往返：
// 密文带 "enc:" 前缀且不等于明文，is_encrypted 正确区分两者，解密还原明文。
func TestModuleCrypto_EncryptDecryptRoundTrip(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	encrypt, ok := mod.Funcs["encrypt"].(func(string) (string, error))
	require.True(t, ok)
	decrypt, ok := mod.Funcs["decrypt"].(func(string) (string, error))
	require.True(t, ok)
	isEncrypted, ok := mod.Funcs["is_encrypted"].(func(string) bool)
	require.True(t, ok)

	const plaintext = "module-crypto round trip payload"
	ciphertext, err := encrypt(plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ciphertext)
	require.True(t, isEncrypted(ciphertext), "ciphertext should be marked encrypted")
	require.False(t, isEncrypted(plaintext), "plaintext should not be marked encrypted")

	decrypted, err := decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

// TestModuleCrypto_EmptyStrings 空串直返分支：encrypt("")/decrypt("") 均返回
// 空串且无错误，is_encrypted("") 为 false。
func TestModuleCrypto_EmptyStrings(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	encrypt, ok := mod.Funcs["encrypt"].(func(string) (string, error))
	require.True(t, ok)
	decrypt, ok := mod.Funcs["decrypt"].(func(string) (string, error))
	require.True(t, ok)
	isEncrypted, ok := mod.Funcs["is_encrypted"].(func(string) bool)
	require.True(t, ok)

	encEmpty, err := encrypt("")
	require.NoError(t, err)
	require.Empty(t, encEmpty)

	decEmpty, err := decrypt("")
	require.NoError(t, err)
	require.Empty(t, decEmpty)

	require.False(t, isEncrypted(""))
}

// TestModuleCrypto_PlaintextPassthrough 非加密串（无 enc: 前缀）在 decrypt 侧
// 原样透传（向后兼容分支）。
func TestModuleCrypto_PlaintextPassthrough(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	decrypt, ok := mod.Funcs["decrypt"].(func(string) (string, error))
	require.True(t, ok)

	passthrough, err := decrypt("not-encrypted-payload")
	require.NoError(t, err)
	require.Equal(t, "not-encrypted-payload", passthrough)
}

// TestModuleCrypto_InvalidCiphertexts 三类非法密文均在 decrypt 侧报错：
// "enc:" 后跟非法 base64、短于 nonce 的密文、认证失败的密文。
func TestModuleCrypto_InvalidCiphertexts(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	decrypt, ok := mod.Funcs["decrypt"].(func(string) (string, error))
	require.True(t, ok)

	invalidInputs := map[string]string{
		"invalid base64": "enc:not-base64!!!",
		"too short":      "enc:AAAA",
		"auth failure":   "enc:" + base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
	for name, input := range invalidInputs {
		out, err := decrypt(input)
		require.Error(t, err, "%s ciphertext should fail to decrypt", name)
		require.Empty(t, out, "%s ciphertext should yield no output", name)
	}
}

// TestModuleCrypto_JSONRoundTrip encrypt_json/decrypt_json 往返：对象、嵌套
// 对象、数组三种 JSON 形态加密后再解密应还原出等值结构（数值解码为 float64）。
func TestModuleCrypto_JSONRoundTrip(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	encryptJSON, ok := mod.Funcs["encrypt_json"].(func(any) (string, error))
	require.True(t, ok)
	decryptJSON, ok := mod.Funcs["decrypt_json"].(func(string) (any, error))
	require.True(t, ok)
	isEncrypted, ok := mod.Funcs["is_encrypted"].(func(string) bool)
	require.True(t, ok)

	values := map[string]any{
		"flat object":   map[string]any{"name": "Alice", "count": 1},
		"nested object": map[string]any{"nested": map[string]any{"x": true}},
		"array":         []any{"a", 2},
	}
	for name, value := range values {
		encrypted, err := encryptJSON(value)
		require.NoError(t, err, "%s should encrypt", name)
		require.True(t, isEncrypted(encrypted), "%s ciphertext should be marked", name)

		decrypted, err := decryptJSON(encrypted)
		require.NoError(t, err, "%s should decrypt", name)
		switch name {
		case "flat object":
			require.Equal(t, map[string]any{"name": "Alice", "count": float64(1)}, decrypted)
		case "nested object":
			require.Equal(t, map[string]any{"nested": map[string]any{"x": true}}, decrypted)
		case "array":
			require.Equal(t, []any{"a", float64(2)}, decrypted)
		}
	}
}

// TestModuleCrypto_EncryptJSONMarshalFailure 含 NaN 的值 JSON 序列化失败：
// encrypt_json 返回空串与错误。
func TestModuleCrypto_EncryptJSONMarshalFailure(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	encryptJSON, ok := mod.Funcs["encrypt_json"].(func(any) (string, error))
	require.True(t, ok)

	out, err := encryptJSON(map[string]any{"nan": math.NaN()})
	require.Error(t, err, "NaN value should fail JSON marshal")
	require.Empty(t, out)
}

// TestModuleCrypto_DecryptJSONBranches decrypt_json 的其余分支：
// 非 "enc:" 前缀的合法 JSON 明文透传后解码为结构（向后兼容）；
// 非 JSON 明文与非法 base64 密文均报错。
func TestModuleCrypto_DecryptJSONBranches(t *testing.T) {
	initModuleCryptoTestEncryptor(t)
	mod := ModuleCrypto()

	decryptJSON, ok := mod.Funcs["decrypt_json"].(func(string) (any, error))
	require.True(t, ok)

	// 明文 JSON：透传 + 解码
	plain, err := decryptJSON(`{"a":1,"b":[true,null]}`)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"a": float64(1), "b": []any{true, nil}}, plain)

	// 非 JSON 明文：unmarshal 失败
	notJSON, err := decryptJSON("plainly not json")
	require.Error(t, err, "non-JSON plaintext should fail unmarshal")
	require.Nil(t, notJSON)

	// 非法 base64 密文：解密失败
	invalid, err := decryptJSON("enc:not-base64!!!")
	require.Error(t, err, "invalid base64 ciphertext should fail")
	require.Nil(t, invalid)
}

// TestModuleCrypto_SHA256Vectors hash_sha256 的标准测试向量：
// "test" 与空串的 SHA-256 十六进制摘要。
func TestModuleCrypto_SHA256Vectors(t *testing.T) {
	mod := ModuleCrypto()

	hashFn, ok := mod.Funcs["hash_sha256"].(func(string) string)
	require.True(t, ok)

	require.Equal(t, "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", hashFn("test"))
	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hashFn(""))
}

package bloom

import (
	"testing"
)

func BenchmarkParseURL(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ParseURL("https://sub.example.com/a/b/c/file.exe?x=1")
	}
}

func BenchmarkGenerateCheckKeys(b *testing.B) {
	keys, _ := ParseURL("https://sub.example.com/a/b/c/file.exe?x=1")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys.GenerateCheckKeys()
	}
}

func BenchmarkDetermineBloomTarget(b *testing.B) {
	keys, _ := ParseURL("https://cdn.example.com/virus.exe?ref=evil")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		determineBloomTarget(keys)
	}
}

func BenchmarkParentPaths(b *testing.B) {
	p := "/a/b/c/d/e/file.txt"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parentPaths(p)
	}
}

func BenchmarkBloomSetAdd(b *testing.B) {
	bs := NewBloomSet(BloomDomain, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bs.Add("source1", "example.com")
	}
}

func BenchmarkBloomSetTest(b *testing.B) {
	bs := NewBloomSet(BloomDomain, 10000)
	bs.Add("source1", "example.com")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bs.Test("example.com")
	}
}

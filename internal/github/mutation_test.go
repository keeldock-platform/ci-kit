package github

import "testing"

func TestExistingPathMutationRights(t *testing.T) {
	for _, right := range []uint32{0x2, 0x4} {
		if pathMutationRights(true)&right != 0 {
			t.Fatal("create-only directory right rejects protected child")
		}
		if pathMutationRights(false)&right == 0 {
			t.Fatal("file write right admitted")
		}
	}
	for _, right := range []uint32{0x40, 0x10000, 0x40000, 0x80000, 0x10000000, 0x40000000} {
		if pathMutationRights(true)&right == 0 {
			t.Fatal("directory replacement right admitted")
		}
	}
}

// SPDX-License-Identifier: MIT

package diff

import (
	"reflect"
	"testing"
)

func TestBaseLines(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  []int
	}{
		{
			name: "one hunk, a changed line",
			patch: "@@ -8,7 +8,7 @@ func a() {\n" +
				" \tx := 1\n" +
				" \ty := 2\n" +
				" \tfoo()\n" +
				"-\tbar()\n" +
				"+\tbar(ctx)\n" +
				" \tbaz()\n" +
				" \treturn\n" +
				" }",
			want: []int{11},
		},
		{
			// The second hunk's numbers come from its own header, not from
			// counting on from the first.
			name: "several hunks",
			patch: "@@ -1,3 +1,1 @@\n" +
				" a\n" +
				"-b\n" +
				"-c\n" +
				"@@ -40,4 +39,4 @@ func b() {\n" +
				" x\n" +
				"-y\n" +
				"+Y\n" +
				" z\n" +
				"-w\n" +
				"+W",
			want: []int{2, 3, 41, 43},
		},
		{
			name: "a pure addition removes nothing",
			patch: "@@ -10,2 +10,3 @@\n" +
				" foo()\n" +
				"+check()\n" +
				" bar()",
			want: nil,
		},
		{
			name:  "a new file removes nothing",
			patch: "@@ -0,0 +1,2 @@\n+a\n+b",
			want:  nil,
		},
		{
			name:  "a pure deletion",
			patch: "@@ -1,3 +0,0 @@\n-a\n-b\n-c",
			want:  []int{1, 2, 3},
		},
		{
			// A count left out of the header is one line.
			name:  "a one-line file replaced",
			patch: "@@ -1 +1 @@\n-x\n+y",
			want:  []int{1},
		},
		{
			name: "no newline at end of file",
			patch: "@@ -4,2 +4,2 @@\n" +
				" keep\n" +
				"-last\n" +
				`\ No newline at end of file` + "\n" +
				"+last\n" +
				`\ No newline at end of file`,
			want: []int{5},
		},
		{
			// A removed SQL comment begins "-- " once its own "-" is added, and
			// is still a removed line, not a file header.
			name:  "a removed line that looks like a header",
			patch: "@@ -3,2 +3,1 @@\n--- the old way\n select 1;",
			want:  []int{3},
		},
		{
			name:  "no patch at all",
			patch: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BaseLines(tt.patch); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BaseLines = %v, want %v", got, tt.want)
			}
		})
	}
}

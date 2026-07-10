package exec

import "testing"

func TestDecodeCLIXMLStderr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "non-clixml passthrough",
			in:   "plain error text\r\n",
			want: "plain error text\r\n",
		},
		{
			name: "progress-only noise drops to empty",
			in: `#< CLIXML
<Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04"><Obj S="progress" RefId="0"><TN RefId="0"><T>System.Management.Automation.PSCustomObject</T></TN><MS><I64 N="SourceId">1</I64><PR N="Record"><AV>正在准备首次使用模块。</AV><AI>0</AI><Nil /><PI>-1</PI><PC>-1</PC><T>Completed</T><SR>-1</SR><SD> </SD></PR></MS></Obj></Objs>`,
			want: "",
		},
		{
			name: "error entries decoded with escapes",
			in: `#< CLIXML
<Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04"><S S="Error">cmdlet not found_x000D__x000A_</S><S S="Error">第二行错误_x000D__x000A_</S><Obj S="progress" RefId="0"></Obj></Objs>`,
			want: "cmdlet not found\r\n第二行错误\r\n",
		},
		{
			name: "unparseable clixml returned verbatim",
			in:   "#< CLIXML\n<Objs broken",
			want: "#< CLIXML\n<Objs broken",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeCLIXMLStderr(tt.in); got != tt.want {
				t.Errorf("DecodeCLIXMLStderr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodePSEscapes(t *testing.T) {
	tests := []struct{ in, want string }{
		{"no escapes", "no escapes"},
		{"a_x000D_b", "a\rb"},
		{"_x000A_", "\n"},
		{"tail_x", "tail_x"},
		{"_xZZZZ_ stays", "_xZZZZ_ stays"},
		{"double_x000D__x000A_end", "double\r\nend"},
	}
	for _, tt := range tests {
		if got := decodePSEscapes(tt.in); got != tt.want {
			t.Errorf("decodePSEscapes(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

package transfer

import "testing"

func TestParseArgs(t *testing.T) {
	tests := []struct {
		src, dst  string
		wantDir   Direction
		wantSrcA  string
		wantSrcP  string
		wantDstA  string
		wantDstP  string
		wantErr   bool
	}{
		{"local.txt", "ali:/tmp/", Upload, "", "local.txt", "ali", "/tmp/", false},
		{"ali:/var/log/app.log", "./", Download, "ali", "/var/log/app.log", "", "./", false},
		{"ali:/data/f.tar", "rn:/backup/", Relay, "ali", "/data/f.tar", "rn", "/backup/", false},
		{"./a", "./b", 0, "", "", "", "", true},
		{"C:/Users/test.txt", "ali:/tmp/", Upload, "", "C:/Users/test.txt", "ali", "/tmp/", false},
		{"C:\\data\\f.txt", "ali:/tmp/", Upload, "", "C:\\data\\f.txt", "ali", "/tmp/", false},
	}

	for _, tt := range tests {
		t.Run(tt.src+"→"+tt.dst, func(t *testing.T) {
			got, err := ParseArgs(tt.src, tt.dst)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Direction != tt.wantDir {
				t.Errorf("direction = %v, want %v", got.Direction, tt.wantDir)
			}
			if got.Src.Alias != tt.wantSrcA || got.Src.Path != tt.wantSrcP {
				t.Errorf("src = %q:%q, want %q:%q", got.Src.Alias, got.Src.Path, tt.wantSrcA, tt.wantSrcP)
			}
			if got.Dst.Alias != tt.wantDstA || got.Dst.Path != tt.wantDstP {
				t.Errorf("dst = %q:%q, want %q:%q", got.Dst.Alias, got.Dst.Path, tt.wantDstA, tt.wantDstP)
			}
		})
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		input     string
		wantAlias string
		wantPath  string
	}{
		{"ali:/tmp/", "ali", "/tmp/"},
		{"./local.txt", "", "./local.txt"},
		{"/absolute/path", "", "/absolute/path"},
		{"C:/Windows/file.txt", "", "C:/Windows/file.txt"},
		{"C:\\data\\f.txt", "", "C:\\data\\f.txt"},
		{"host-name:/path", "host-name", "/path"},
		{"host_name:/path", "host_name", "/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ep := parseEndpoint(tt.input)
			if ep.Alias != tt.wantAlias {
				t.Errorf("alias = %q, want %q", ep.Alias, tt.wantAlias)
			}
			if ep.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", ep.Path, tt.wantPath)
			}
		})
	}
}

package sysperm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNeedsFix(t *testing.T) {
	cases := []struct {
		name string
		st   Status
		want bool
	}{
		{"没有设备", Status{Supported: true}, false},
		{"有设备但都能读写", Status{Supported: true, Devices: []string{"/dev/ttyUSB0"}}, false},
		{"有一个读不到", Status{Supported: true, Devices: []string{"/dev/ttyUSB0"}, Denied: []string{"/dev/ttyUSB0"}}, true},
		{"不支持", Status{}, false},
	}
	for _, c := range cases {
		if got := c.st.NeedsFix(); got != c.want {
			t.Fatalf("%s: NeedsFix = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// 空列表必须序列化成 []，不能是 null：前端对 null 做 .length 会抛，
// 而 Probe() 在没有设备时返回的正是空列表。
func TestStatusJSONUsesArrays(t *testing.T) {
	b, err := json.Marshal(Probe())
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	s := string(b)
	for _, field := range []string{"devices", "denied"} {
		if strings.Contains(s, `"`+field+`":null`) {
			t.Fatalf("%s 序列化成了 null: %s", field, s)
		}
		if !strings.Contains(s, `"`+field+`":`) {
			t.Fatalf("缺少字段 %s: %s", field, s)
		}
	}
}

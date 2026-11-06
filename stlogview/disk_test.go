package stlogview

import "testing"

// TestDiskLaneMask 覆盖第三条泳道（落盘）的三种状态：
//
//	bit0 = 这一段里有已落盘的帧，bit1 = 有没落盘的帧
//	1 = 全落（绿）  2 = 全没落（透明）  3 = 两种都有（黄）
//
// 落盘是会话级开关，中途打开/关掉就要在泳道上留下分界，所以要按桶算出来。
func TestDiskLaneMask(t *testing.T) {
	s := newLiveDoc(t, 1<<20)
	payload := []byte("aaaaaaaa")

	// 前 41 帧不落盘（默认就是关的）。
	for i := 0; i < 41; i++ {
		s.AppendFrame(liveFrame(int64(1000+i*10), 1, payload))
	}
	s.SetRecording(true)
	for i := 41; i < 80; i++ {
		s.AppendFrame(liveFrame(int64(1000+i*10), 1, payload))
	}

	// 32 个桶、80 帧 → 每桶 2.5 帧；第 16 桶正好跨在切换点上。
	r := s.Ruler(32)
	if len(r.Buckets) != 32 {
		t.Fatalf("桶数 %d", len(r.Buckets))
	}
	if got := r.Buckets[0].Disk; got != 2 {
		t.Fatalf("开头应全是没落盘（2），得到 %d", got)
	}
	if got := r.Buckets[16].Disk; got != 3 {
		t.Fatalf("跨切换点的桶应同时置两位（3），得到 %d", got)
	}
	if got := r.Buckets[31].Disk; got != 1 {
		t.Fatalf("末尾应全是已落盘（1），得到 %d", got)
	}

	// 清空之后帧号从头算：当前状态（在录）保留，旧的分界作废。
	// 只有两帧、桶却有 32 个，空桶的 mask 是 0（不画），有帧的桶必须是「全落盘」。
	s.ResetLive()
	s.AppendFrames([]FrameIn{
		liveFrame(9000, 1, payload),
		liveFrame(9010, 1, payload),
	})
	r = s.Ruler(32)
	seen := 0
	for i, b := range r.Buckets {
		if b.Disk == 0 {
			continue
		}
		if b.Disk != 1 {
			t.Fatalf("清空后第 %d 桶应为全落盘（1），得到 %d", i, b.Disk)
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("清空后应至少有一个桶带帧")
	}
}

// TestDiskLanePrepend 往回补历史时，接缝前的帧一定在磁盘上（它们是从会话文件
// 读回来的），接缝之后回到原来的状态 —— 帧号整体后移，切换点也要跟着挪。
func TestDiskLanePrepend(t *testing.T) {
	s := newLiveDoc(t, 1<<20)
	payload := []byte("aaaaaaaa")
	for i := 0; i < 40; i++ {
		s.AppendFrame(liveFrame(int64(5000+i*10), 1, payload))
	}
	if got := s.recordingLocked(); got {
		t.Fatal("新文档默认应是「不落盘」")
	}
	s.PrependFrames([]FrameIn{
		liveFrame(4000, 1, payload),
		liveFrame(4010, 1, payload),
	})
	if s.LiveInfo().Total != 42 {
		t.Fatalf("前插之后应有 42 帧，得到 %d", s.LiveInfo().Total)
	}
	if !s.diskAtLocked(0) || !s.diskAtLocked(1) {
		t.Fatal("补回来的历史帧应视为已落盘")
	}
	if s.diskAtLocked(2) || s.diskAtLocked(41) {
		t.Fatal("接缝之后的帧应保持原来的「没落盘」")
	}
}

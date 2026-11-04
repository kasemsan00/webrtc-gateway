package session

// h264SPSPictureSize returns the cropped display size from a single SPS NAL.
// Baseline and Constrained Baseline (profile_idc 66) are parsed fully. Other
// profiles return ok=false so a failed parse is not treated as a size change.
func h264SPSPictureSize(nal []byte) (width int, height int, ok bool) {
	if len(nal) < 4 || nal[0]&0x1f != 7 {
		return 0, 0, false
	}
	rbsp := stripEmulationPrevention(nal[1:])
	r := bitReader{data: rbsp}
	profile, ok := r.u(8)
	if !ok {
		return 0, 0, false
	}
	if _, ok = r.u(8); !ok { // constraint flags
		return 0, 0, false
	}
	if _, ok = r.u(8); !ok { // level_idc
		return 0, 0, false
	}
	if _, ok = r.ue(); !ok { // seq_parameter_set_id
		return 0, 0, false
	}
	chroma := uint(1)
	if profile == 100 || profile == 110 || profile == 122 || profile == 244 || profile == 44 || profile == 83 || profile == 86 || profile == 118 || profile == 128 || profile == 138 || profile == 139 || profile == 134 || profile == 135 {
		chroma, ok = r.ue()
		if !ok {
			return 0, 0, false
		}
		if chroma == 3 {
			if _, ok = r.u(1); !ok {
				return 0, 0, false
			}
		}
		if _, ok = r.ue(); !ok { // bit_depth_luma_minus8
			return 0, 0, false
		}
		if _, ok = r.ue(); !ok { // bit_depth_chroma_minus8
			return 0, 0, false
		}
		if _, ok = r.u(1); !ok { // qpprime_y_zero_transform_bypass_flag
			return 0, 0, false
		}
		scaling, bitOK := r.u(1)
		if !bitOK {
			return 0, 0, false
		}
		if scaling != 0 {
			return 0, 0, false
		}
	}
	if _, ok = r.ue(); !ok { // log2_max_frame_num_minus4
		return 0, 0, false
	}
	pocType, ok := r.ue()
	if !ok {
		return 0, 0, false
	}
	switch pocType {
	case 0:
		if _, ok = r.ue(); !ok { // log2_max_pic_order_cnt_lsb_minus4
			return 0, 0, false
		}
	case 1:
		return 0, 0, false
	case 2:
	default:
		return 0, 0, false
	}
	if _, ok = r.ue(); !ok { // max_num_ref_frames
		return 0, 0, false
	}
	if _, ok = r.u(1); !ok { // gaps_in_frame_num_value_allowed_flag
		return 0, 0, false
	}
	widthMBs, ok := r.ue()
	if !ok {
		return 0, 0, false
	}
	heightUnits, ok := r.ue()
	if !ok {
		return 0, 0, false
	}
	frameMBsOnly, ok := r.u(1)
	if !ok {
		return 0, 0, false
	}
	if frameMBsOnly == 0 {
		if _, ok = r.u(1); !ok { // mb_adaptive_frame_field_flag
			return 0, 0, false
		}
	}
	if _, ok = r.u(1); !ok { // direct_8x8_inference_flag
		return 0, 0, false
	}
	cropping, ok := r.u(1)
	if !ok {
		return 0, 0, false
	}
	cropLeft, cropRight, cropTop, cropBottom := uint(0), uint(0), uint(0), uint(0)
	if cropping != 0 {
		cropLeft, ok = r.ue()
		if !ok {
			return 0, 0, false
		}
		cropRight, ok = r.ue()
		if !ok {
			return 0, 0, false
		}
		cropTop, ok = r.ue()
		if !ok {
			return 0, 0, false
		}
		cropBottom, ok = r.ue()
		if !ok {
			return 0, 0, false
		}
	}

	cropUnitX := uint(1)
	cropUnitY := uint(2 - frameMBsOnly)
	if chroma == 1 || chroma == 2 {
		cropUnitX = 2
	}
	if chroma == 1 {
		cropUnitY = 2 * (2 - frameMBsOnly)
	}
	picWidth := (widthMBs + 1) * 16
	picHeight := (heightUnits + 1) * 16 * (2 - frameMBsOnly)
	if picWidth <= (cropLeft+cropRight)*cropUnitX || picHeight <= (cropTop+cropBottom)*cropUnitY {
		return 0, 0, false
	}
	return int(picWidth - (cropLeft+cropRight)*cropUnitX), int(picHeight - (cropTop+cropBottom)*cropUnitY), true
}

func h264SPSUnits(payload []byte) [][]byte {
	if len(payload) == 0 {
		return nil
	}
	nalType := payload[0] & 0x1f
	if nalType == 7 {
		return [][]byte{payload}
	}
	if nalType != 24 || len(payload) < 3 {
		return nil
	}
	var units [][]byte
	offset := 1
	for offset+2 <= len(payload) {
		size := int(payload[offset])<<8 | int(payload[offset+1])
		offset += 2
		if size <= 0 || offset+size > len(payload) {
			break
		}
		unit := payload[offset : offset+size]
		offset += size
		if len(unit) > 0 && unit[0]&0x1f == 7 {
			units = append(units, unit)
		}
	}
	return units
}

func stripEmulationPrevention(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp))
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b == 0x03 {
			zeros = 0
			continue
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

type bitReader struct {
	data []byte
	pos  int
}

func (r *bitReader) bit() (uint, bool) {
	if r.pos >= len(r.data)*8 {
		return 0, false
	}
	b := r.data[r.pos/8]
	shift := 7 - (r.pos % 8)
	r.pos++
	return uint((b >> shift) & 1), true
}

func (r *bitReader) u(n int) (uint, bool) {
	if n < 0 || n > 32 {
		return 0, false
	}
	var value uint
	for i := 0; i < n; i++ {
		bit, ok := r.bit()
		if !ok {
			return 0, false
		}
		value = (value << 1) | bit
	}
	return value, true
}

func (r *bitReader) ue() (uint, bool) {
	zeros := 0
	for {
		bit, ok := r.bit()
		if !ok {
			return 0, false
		}
		if bit == 1 {
			break
		}
		zeros++
		if zeros > 31 {
			return 0, false
		}
	}
	if zeros == 0 {
		return 0, true
	}
	rest, ok := r.u(zeros)
	if !ok {
		return 0, false
	}
	return (1 << zeros) - 1 + rest, true
}

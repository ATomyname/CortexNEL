package captchaocr

// 置信度评估：logit top1-top2 边距越小，识别越可能出错
// （2026-09 实测真实验证码边距分布 0.95~4.13，低边距图与登录失败强相关）。

// RecognizeWithConfidence 识别并返回最小边距，供调用方门控换图重试。
func (r *Recognizer) RecognizeWithConfidence(imgBytes []byte) (string, float32, error) {
	code, err := r.Recognize(imgBytes)
	if err != nil {
		return "", 0, err
	}
	minMargin := float32(1e9)
	for _, row := range r.LastLogits {
		best, second := float32(-1e9), float32(-1e9)
		for _, v := range row {
			switch {
			case v > best:
				second = best
				best = v
			case v > second:
				second = v
			}
		}
		if m := best - second; m < minMargin {
			minMargin = m
		}
	}
	return code, minMargin, nil
}

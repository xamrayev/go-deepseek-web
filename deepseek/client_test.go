package deepseek

import (
	"encoding/json"
	"testing"
)

func TestParseChallengeJSON(t *testing.T) {
	raw := `{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"challenge":{"algorithm":"DeepSeekHashV1","challenge":"e3100e4b312dff68c1558950048401049b6a5af6977854a0e70182e0fe82150f","salt":"f440d547486aa66b435e","signature":"0c8e6593729ddb19d8213c991006dc23c8d9b8e4b94a3d1395f48dbd3a5c016b","difficulty":144000,"expire_at":1791982051472,"expire_after":300000,"target_path":"/api/v0/chat/completion"}}}}`

	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BizData map[string]interface{} `json:"biz_data"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	targetMap := res.Data.BizData
	if sub, ok := targetMap["challenge"].(map[string]interface{}); ok {
		targetMap = sub
	}

	chal := PowChallenge{
		Algorithm:  getString(targetMap, "algorithm", "DeepSeekHashV1"),
		Challenge:  getString(targetMap, "challenge", ""),
		Salt:       getString(targetMap, "salt", ""),
		Difficulty: getInt64(targetMap, "difficulty", 144000),
		ExpireAt:   getInt64(targetMap, "expire_at", 0),
		Signature:  getString(targetMap, "signature", ""),
		TargetPath: getString(targetMap, "target_path", "/api/v0/chat/completion"),
	}

	t.Logf("Parsed Challenge: %+v", chal)

	if chal.Challenge == "" {
		t.Fatalf("challenge should not be empty")
	}
	if chal.Difficulty != 144000 {
		t.Fatalf("difficulty want 144000, got %d", chal.Difficulty)
	}
	if chal.ExpireAt != 1791982051472 {
		t.Fatalf("expire_at want 1791982051472, got %d", chal.ExpireAt)
	}

	ansB64, err := SolvePoW(chal)
	if err != nil {
		t.Fatalf("SolvePoW failed: %v", err)
	}

	t.Logf("Solved PoW Base64: %s", ansB64)
}

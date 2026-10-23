package notify

import (
	"fmt"

	"github.com/m1ngsama/filebox/internal/db"
)

func humanBytes(n int64) string {
	u := []string{"B", "KB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, u[i])
}

// Text says in one line what happened.
func Text(lang string, e db.Event) string {
	zh := lang == "zh"
	pick := func(en, cn string) string {
		if zh {
			return cn
		}
		return en
	}
	where := e.Target
	switch e.Kind {
	case "test":
		return pick("This is a test notification from filebox.", "这是一条来自 filebox 的测试通知。")
	case db.EventUpload:
		return pick(fmt.Sprintf("%s was uploaded (%s)", e.Name, humanBytes(e.Size)), fmt.Sprintf("上传了 %s（%s）", e.Name, humanBytes(e.Size)))
	case db.EventDownload:
		return pick(fmt.Sprintf("%s was downloaded from a shared link", e.Name), fmt.Sprintf("有人通过分享链接下载了 %s", e.Name))
	case db.EventLogin:
		return pick("Someone signed in", "有人登录了")
	case db.EventLoginFailed:
		return pick("A sign-in attempt failed", "有一次登录失败")
	case db.EventShareCreate:
		if where == "" {
			where = e.Name
		}
		return pick("A share link was created for "+where, "创建了分享链接："+where)
	case db.EventTokenCreate:
		return pick("An app password was created: "+e.Name, "创建了应用密码："+e.Name)
	case EventDiskLow:
		return pick(fmt.Sprintf("Volume %s is running low on space: %s free", e.Vol, humanBytes(e.Size)), fmt.Sprintf("卷 %s 空间不足，只剩 %s", e.Vol, humanBytes(e.Size)))
	}
	return e.Kind
}

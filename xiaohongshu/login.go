package xiaohongshu

import (
	"context"
	"time"

	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type LoginAction struct {
	page *rod.Page
}

func NewLogin(page *rod.Page) *LoginAction {
	return &LoginAction{page: page}
}

func (a *LoginAction) CheckLoginStatus(ctx context.Context) (bool, error) {
	pp := a.page.Context(ctx)
	applySiteLocale(pp)
	pp.MustNavigate(Site().Home).MustWaitLoad()

	time.Sleep(1 * time.Second)

	exists, _, err := pp.Has(Site().LoggedInSel)
	if err != nil {
		return false, errors.Wrap(err, "check login status failed")
	}

	return exists, nil
}

// Login 打开站点首页,等待用户在窗口中完成登录(扫码/手机验证码均可)。
// 轮询已登录标志,最长等待 loginWaitTimeout;比旧版的单次 MustElement 更鲁棒。
const loginWaitTimeout = 10 * time.Minute

func (a *LoginAction) Login(ctx context.Context) error {
	pp := a.page.Context(ctx)
	applySiteLocale(pp)
	pp.MustNavigate(Site().Home).MustWaitLoad()

	time.Sleep(2 * time.Second)

	if exists, _, _ := pp.Has(Site().LoggedInSel); exists {
		return nil
	}

	logrus.Infof("请在浏览器窗口中完成 %s 登录(扫码或手机验证码),最长等待 %v...", Site().Name, loginWaitTimeout)

	deadline := time.NewTimer(loginWaitTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.Errorf("等待登录超时(%v)", loginWaitTimeout)
		case <-ticker.C:
			if exists, _, _ := pp.Has(Site().LoggedInSel); exists {
				logrus.Info("检测到登录成功")
				// 稍等让会话 cookie 全部落定
				time.Sleep(2 * time.Second)
				return nil
			}
		}
	}
}

func (a *LoginAction) FetchQrcodeImage(ctx context.Context) (string, bool, error) {
	pp := a.page.Context(ctx)
	applySiteLocale(pp)

	pp.MustNavigate(Site().Home).MustWaitLoad()

	time.Sleep(2 * time.Second)

	if exists, _, _ := pp.Has(Site().LoggedInSel); exists {
		return "", true, nil
	}

	// 获取二维码图片(海外站可能无扫码入口,此时返回错误,请改用窗口手动登录)
	src, err := pp.MustElement(".login-container .qrcode-img").Attribute("src")
	if err != nil {
		return "", false, errors.Wrap(err, "get qrcode src failed")
	}
	if src == nil || len(*src) == 0 {
		return "", false, errors.New("qrcode src is empty")
	}

	return *src, false, nil
}

func (a *LoginAction) WaitForLogin(ctx context.Context) bool {
	pp := a.page.Context(ctx)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			el, err := pp.Element(Site().LoggedInSel)
			if err == nil && el != nil {
				return true
			}
		}
	}
}

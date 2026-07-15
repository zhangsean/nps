package conn

import "time"

type Secret struct {
	Password string
	Conn     *Conn
}

func NewSecret(p string, conn *Conn) *Secret {
	return &Secret{
		Password: p,
		Conn:     conn,
	}
}

type Link struct {
	ConnType               string //连接类型
	Host                   string //目标
	Crypt                  bool   //加密
	Compress               bool
	LocalProxy             bool
	RemoteAddr             string
	Option                 Options
	TargetConnectRetryHook TargetConnectRetryHook `json:"-"`
}

type Option func(*Options)

type RetryInfo struct {
	Source   string
	ConnType string
	Target   string
	Attempt  int
	Attempts int
	Delay    time.Duration
	Error    string
}

type TargetConnectRetryHook func(RetryInfo)

type Options struct {
	Timeout       time.Duration
	RetryCount    int
	RetryInterval time.Duration
}

var defaultTimeOut = time.Second * 5

func NewLink(connType string, host string, crypt bool, compress bool, remoteAddr string, localProxy bool, opts ...Option) *Link {
	options := newOptions(opts...)

	return &Link{
		RemoteAddr: remoteAddr,
		ConnType:   connType,
		Host:       host,
		Crypt:      crypt,
		Compress:   compress,
		LocalProxy: localProxy,
		Option:     options,
	}
}

func newOptions(opts ...Option) Options {
	opt := Options{
		Timeout: defaultTimeOut,
	}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

func LinkTimeout(t time.Duration) Option {
	return func(opt *Options) {
		opt.Timeout = t
	}
}

func LinkRetryCount(retryCount int) Option {
	return func(opt *Options) {
		opt.RetryCount = retryCount
	}
}

func LinkRetryInterval(retryInterval time.Duration) Option {
	return func(opt *Options) {
		opt.RetryInterval = retryInterval
	}
}

func (l *Link) SetTargetConnectRetryHook(hook TargetConnectRetryHook) {
	if l != nil {
		l.TargetConnectRetryHook = hook
	}
}

func (l *Link) TriggerTargetConnectRetry(info RetryInfo) {
	if l != nil && l.TargetConnectRetryHook != nil {
		l.TargetConnectRetryHook(info)
	}
}

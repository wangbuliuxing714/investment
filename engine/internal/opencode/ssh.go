package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func (c *Client) ensure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return down("SSH 命令超时")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ssh != nil {
		session, err := c.ssh.NewSession()
		if err == nil {
			_ = session.Close()
			return nil
		}
		c.dropLocked()
	}
	if err := c.dialLocked(); err != nil {
		return err
	}
	if c.ln == nil {
		ln, err := net.Listen("tcp", c.Local)
		if err != nil {
			c.dropLocked()
			return down("本机端口 " + c.Local + " 没有打开")
		}
		c.ln = ln
		go c.accept()
	}
	return nil
}

func (c *Client) dialLocked() error {
	if _, err := os.Stat(c.KeyPath); err != nil {
		return keyReadError(err)
	}
	key, err := os.ReadFile(c.KeyPath)
	if err != nil {
		return keyReadError(err)
	}
	signer, err := signerFrom(key, c.Passphrase)
	if err != nil {
		return down("SSH 密钥没有解开")
	}
	home, _ := os.UserHomeDir()
	callback, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		return down("没有本机的 SSH 主机记录")
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: callback,
		Timeout:         20 * time.Second,
	}
	addr := c.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "22")
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return down("SSH 没有连上服务器")
	}
	c.ssh = client
	c.sshDone = make(chan struct{})
	go keepAlive(client, c.sshDone)
	return nil
}

func keyReadError(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return down("引擎无权读取 SSH 密钥，请以有权访问该密钥的用户运行引擎")
	}
	if errors.Is(err, os.ErrNotExist) {
		return down("SSH 密钥文件不存在，请核对 DESK_SSH_KEY 路径")
	}
	return down("SSH 密钥读取失败，请检查路径和文件访问权限")
}

func signerFrom(key []byte, passphrase string) (ssh.Signer, error) {
	if passphrase == "" {
		return ssh.ParsePrivateKey(key)
	}
	return ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
}

func keepAlive(client *ssh.Client, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) dropLocked() {
	if c.sshDone != nil {
		close(c.sshDone)
		c.sshDone = nil
	}
	if c.ssh != nil {
		_ = c.ssh.Close()
		c.ssh = nil
	}
}

func (c *Client) accept() {
	for {
		local, err := c.ln.Accept()
		if err != nil {
			return
		}
		go c.bridge(local)
	}
}

func (c *Client) bridge(local net.Conn) {
	defer local.Close()
	c.mu.Lock()
	client := c.ssh
	remoteAddr := c.Remote
	c.mu.Unlock()
	if client == nil {
		return
	}
	remote, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
}

func (c *Client) run(ctx context.Context, cmd string, stdin []byte) ([]byte, error) {
	c.mu.Lock()
	client := c.ssh
	c.mu.Unlock()
	if client == nil {
		return nil, down("SSH 还没有连上")
	}
	session, err := client.NewSession()
	if err != nil {
		return nil, down("SSH 会话没有建立")
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if stdin != nil {
		session.Stdin = bytes.NewReader(stdin)
	}
	if err := session.Start(cmd); err != nil {
		return nil, down("SSH 命令没有执行")
	}
	waited := make(chan error, 1)
	go func() { waited <- session.Wait() }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return nil, down("SSH 命令超时")
	case err := <-waited:
		if err != nil {
			return nil, down(fmt.Sprintf("SSH 命令失败：%s", clip(stderr.String())))
		}
		return stdout.Bytes(), nil
	}
}

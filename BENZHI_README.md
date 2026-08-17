# task034-snapdiff

这是一个纯 Go 的目录快照对比 HTTP 服务。客户端可以提交带内容和模式信息的文件快照，并比较两个快照中的新增、删除、内容修改、元数据变化、重命名和未变化文件。服务只依赖 Go 标准库，不需要数据库或外部服务。

## 标准命令

在 `env/` 目录执行：

```bash
go build ./...
go test ./...
go vet ./...
go run . --smoke-test
go run . server --addr :8080
```

`--smoke-test` 会启动进程内 HTTP 服务并完成自检后退出；服务器模式默认监听 `:8080`。

## Benzhi 容器

`build_benzhi_docker.sh` 使用 `benzhi.Dockerfile` 构建评测镜像，参数依次是镜像名和平台，默认值为 `my-project` 与 `linux/amd64`。例如：

```bash
bash build_benzhi_docker.sh snapdiff-benzhi linux/amd64
docker run --rm -it snapdiff-benzhi:latest
```

容器启动后进入 shell；构建阶段会执行 `go build ./...`，不访问外部业务服务。

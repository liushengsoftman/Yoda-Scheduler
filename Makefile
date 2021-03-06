all: local

local: fmt vet
	GOOS=linux GOARCH=amd64 go build  -o=bin/yoda-scheduler ./cmd/scheduler

build:  local
	docker build --no-cache . -t registry.cn-hangzhou.aliyuncs.com/njupt-isl/yoda-scheduler:3.0

push:
	docker push registry.cn-hangzhou.aliyuncs.com/njupt-isl/yoda-scheduler:3.0

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

clean: fmt vet
	sudo rm -f yoda-scheduler

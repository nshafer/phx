default:
	echo "make publish ver=vX.X.X"

publish:
ifndef ver
	$(error must give ver=vX.X.X)
endif
	go mod tidy
	git tag $(ver)
	git push origin --tags
	GOPROXY=proxy.golang.org go list -m github.com/nshafer/phx@$(ver)

default:
	echo "make publish ver=vX.X.X"

publish:
ifndef ver
	$(error must give ver=vX.X.X)
endif
	go mod tidy
	git tag $(ver)
	git push origin
	git push origin --tags
	GOPROXY=proxy.golang.org go list -m github.com/nshafer/phx@$(ver)
	http -b https://proxy.golang.org/github.com/nshafer/phx/@v/$(ver).info
	echo "Go add changelog to Releases: https://github.com/nshafer/phx/releases"

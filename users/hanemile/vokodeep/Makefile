SOURCE = vokodeep.lisp deploy.lisp

.PHONY: all deploy clean

all: deploy
deploy: vokodeep
clean:
	rm -f build/* intertube-scraper intertube-web *.fasl

vokodeep: $(SOURCE)
	sbcl --noinform --no-sysinit --no-userinit --disable-debugger --script deploy.lisp

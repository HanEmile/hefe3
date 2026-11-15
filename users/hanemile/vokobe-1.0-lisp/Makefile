SOURCE = vokobe2.lisp deploy.lisp

.PHONY: all deploy clean

all: deploy
deploy: vokobe2
clean:
	rm -f build/* intertube-scraper intertube-web *.fasl

vokobe2: $(SOURCE)
	sbcl --noinform --no-sysinit --no-userinit --disable-debugger --script deploy.lisp

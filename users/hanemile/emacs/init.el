;; Minimal emacs configuration file
;; https://www.rahuljuliato.com/posts/emacs-solo-two-year

(use-package emacs
	:ensure nil
	:bind
	(("M-0" . other-window)
	 ("M-s g" . grep))
	:custom
	(inhibit-startup-message t)
	(initial-scratch-message "")
	(xterm-mouse-mode 1))

(load-theme 'leuven)

(load-theme 'leuven t)

; get rid of the emacs bloat
(menu-bar-mode -1)
(setq inhibit-startup-message t) 
(setq initial-scratch-message nil)

(setq inferior-lisp-program "sbcl --no-userinit")

(custom-set-variables
 ;; custom-set-variables was added by Custom.
 ;; If you edit it by hand, you could mess it up, so be careful.
 ;; Your init file should contain only one such instance.
 ;; If there is more than one, they won't work right.
 '(Package-selected-packages
   '(sly magit whitespace-cleanup-mode nix-mode modus-themes smartparens paredit multiple-cursors evil dockerfile-mode slime crux))
 '(lisp-mode-hook '(sly-editing-mode))
 '(package-selected-packages
   '(which-key magit whitespace-cleanup-mode nix-mode modus-themes smartparens paredit multiple-cursors evil dockerfile-mode crux sly)))
(custom-set-faces
 ;; custom-set-faces was added by Custom.
 ;; If you edit it by hand, you could mess it up, so be careful.
 ;; Your init file should contain only one such instance.
 ;; If there is more than one, they won't work right.
 )

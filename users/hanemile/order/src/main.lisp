(in-package :order)

(defun main ()
  (let* ((raw-args (rest sb-ext:*posix-argv*))
         (full (member "--full" raw-args :test #'string=))
         (args (remove "--full" raw-args :test #'string=)))
    (when (or (< (length args) 3) (member "--help" args :test #'string=) (member "-h" args :test #'string=))
      (format t "Usage: order [--full] <input-dir> <output-dir> <site-name> [visibility]~%")
      (sb-ext:exit :code (if (< (length args) 3) 1 0)))
    (let ((input (first args)) (output (second args)) (site (third args))
          (vis (or (fourth args) "private")))
      (format t "order: ~A [~A] ~A → ~A~%" site vis input output)
      (let ((start (get-internal-real-time)))
        (build-site :input-dir input :output-dir output :site-name site
                    :visibility vis :full (if full t nil))
        (format t "~%Build completed in ~,3Fs~%"
                (/ (- (get-internal-real-time) start) (float internal-time-units-per-second)))))))

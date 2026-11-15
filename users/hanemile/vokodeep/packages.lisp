(defpackage :vokodeep
  (:use #:cl
        #:alexandria
        #:split-sequence
        #:cl-ppcre
        #:bordeaux-threads)
  (:export #:process-folder #:define-plugin))

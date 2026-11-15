(defsystem :custom-lib
    :components ((:file "custom-lib"))
    :depends-on (:iterate :borda-count))

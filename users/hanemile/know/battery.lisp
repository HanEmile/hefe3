(defpackage :battery
  (:use :cl :know)
  (:local-nicknames (:k :know))))

(in-package :battery)

(k:obj
   (k:prop :type "battery")
   (k:prop :name "LiFePo Battery")
   (k:prop :battery-type "Lithium")
   (k:prop :chemical (k:chem Li Fe Po)
   (k:prop :Description "LiFePo"))

(in-package :order)

(defun sanitize (string)
  "Convert STRING to a URL-safe anchor: lowercase, spaces to hyphens, keep alnum and hyphens."
  (string-downcase
   (remove-if-not (lambda (c) (or (alphanumericp c) (char= c #\-)))
                  (substitute #\- #\Space string))))

(defun read-file (path)
  (with-open-file (s path :direction :input :if-does-not-exist nil :external-format :utf-8)
    (when s (let* ((buf (make-string (file-length s))) (n (read-sequence buf s)))
              (subseq buf 0 n)))))

(defun write-file (path content)
  (ensure-directories-exist path)
  (with-open-file (s path :direction :output :if-exists :supersede) (write-string content s)))

(defun split-string (string &optional (delimiter #\Newline))
  (loop for start = 0 then (1+ pos)
        for pos = (position delimiter string :start start)
        collect (subseq string start (or pos (length string))) while pos))

(defun join-strings (strings &optional (separator ""))
  (format nil (concatenate 'string "~{~A~^" separator "~}") strings))

(defun starts-with-p (string prefix)
  (and (>= (length string) (length prefix)) (string= string prefix :end1 (length prefix))))

(defun ensure-trailing-slash (path)
  (if (and (> (length path) 0) (char= (char path (1- (length path))) #\/))
      path (concatenate 'string path "/")))

(defun relative-path (path base)
  (let ((ps (namestring path))
        (bs (namestring (ensure-trailing-slash (namestring base)))))
    (if (starts-with-p ps bs) (subseq ps (length bs)) ps)))

(defun path-components (path-string)
  (remove-if (lambda (s) (string= s "")) (split-string path-string #\/)))

(defun format-universal-time (ut)
  (multiple-value-bind (sec min hour day month year) (decode-universal-time ut)
    (declare (ignore sec min hour))
    (format nil "~4,'0D-~2,'0D-~2,'0D" year month day)))

(defun substitute-all (string old new)
  (with-output-to-string (s)
    (let ((ol (length old)) (i 0) (len (length string)))
      (loop while (< i len)
            do (if (and (<= (+ i ol) len) (string= string old :start1 i :end1 (+ i ol)))
                   (progn (write-string new s) (incf i ol))
                   (progn (write-char (char string i) s) (incf i)))))))

(defun escape-html (string &optional include-quotes)
  (let ((s (substitute-all (substitute-all (substitute-all string "&" "&amp;") "<" "&lt;") ">" "&gt;")))
    (if include-quotes (substitute-all s "\"" "&quot;") s)))

(defun visibility-level (vis)
  (cond ((string-equal vis "public") 1) ((string-equal vis "internal") 2) (t 3)))

(defun allowed-visibility-tiers (max-visibility)
  (cond ((string-equal max-visibility "public") '("public"))
        ((string-equal max-visibility "internal") '("public" "internal"))
        (t '("public" "internal" "private"))))

(defun visibility-visible-p (object-vis build-vis)
  (member object-vis (allowed-visibility-tiers build-vis) :test #'string-equal))

(defun date-string-p (s)
  (and (stringp s) (= (length s) 10) (char= (char s 4) #\-) (char= (char s 7) #\-)
       (loop for i from 0 below 10 always (or (= i 4) (= i 7) (digit-char-p (char s i))))))

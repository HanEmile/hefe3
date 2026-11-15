(in-package :vokobe3)

;; --- Configuration ---
(defparameter *input-dir* (ensure-directory-pathname "in/"))
(defparameter *output-dir* (ensure-directory-pathname "out/"))
(defparameter *site-name* "λ-Wiki")
(defparameter *pages* (make-hash-table :test 'equal))
(defparameter *last-mod* (make-hash-table :test 'equal))

(defparameter *style*
  "body { font-family: monospace; line-height: 1.5; max-width: 800px; margin: 40px auto; padding: 0 20px; background: #fdfdfd; color: #333; }
   header { margin-bottom: 2em; border-bottom: 1px solid #ddd; padding-bottom: 1em; }
   footer { margin-top: 3em; color: #888; font-size: 0.8em; }
   pre { background: #f4f4f4; padding: 1em; border-left: 3px solid #0077aa; overflow-x: auto; }
   .toc { background: #fafafa; padding: 1em; border: 1px solid #eee; margin-bottom: 2em; }
   .backlinks { margin-top: 2em; padding: 1em; background: #f9f9f9; border-top: 1px solid #ddd; }
   a { color: #0077aa; text-decoration: none; }
   a:hover { text-decoration: underline; }")

;; --- Core Logic ---
(defstruct page name content backlinks toc)

(defun sanitize (str)
  (string-downcase (regex-replace-all "[^a-zA-Z0-9]+" str "-")))

(defun parse-markdown (text)
  "Parses markdown and generates hierarchical numbers for headings."
  (let ((toc-acc nil)
        (counters (make-array 7 :initial-element 0)))
    (values 
     (with-output-to-string (s)
       (dolist (line (split "\\n" text))
         (cond
           ((scan "^(#+)\\s*(.*)" line)
            (register-groups-bind (hashes title) ("^(#+)\\s*(.*)" line)
              (let* ((lvl (length hashes))
                     (id (sanitize title)))
                (incf (aref counters lvl))
                (loop for i from (1+ lvl) to 6 do (setf (aref counters i) 0))
                (let ((num-str (with-output-to-string (ns)
                                 (loop for i from 1 to lvl
                                       do (format ns "~D." (aref counters i)))
                                 (format ns " "))))
                  (push (list :lvl lvl :title title :id id :num num-str) toc-acc)
                  (format s "<h~D id='~A'><a href='#~A'>~A~A</a></h~D>~%" 
                          lvl id id num-str title lvl)))))
           ((scan ":::toc" line) (format s "[[TOC]]"))
           ((scan ":::backlinks" line) (format s "[[BACK]]"))
           (t (let ((p (regex-replace-all "\\[(.+?)\\]\\((.+?)\\)" line "<a href='\\2.html'>\\1</a>")))
                (format s "~A<br>~%" p))))))
     (reverse toc-acc))))

(defun build-state ()
  (clrhash *pages*)
  (ensure-directories-exist *input-dir*)
  (ensure-directories-exist *output-dir*)
  (dolist (file (directory-files *input-dir* "*.md"))
    (let ((name (pathname-name file)))
      (setf (gethash name *pages*) (make-page :name name :content (read-file-string file)))))
  (maphash (lambda (name page)
             (multiple-value-bind (html toc) (parse-markdown (page-content page))
               (declare (ignore html))
               (setf (page-toc page) toc))
             (setf (page-backlinks page) 
                   (loop for n being the hash-keys of *pages* using (hash-value p)
                         when (and (not (string= n name)) (scan (format nil "\\(~A\\)" name) (page-content p)))
                         collect n)))
           *pages*))

(defun render-page (name)
  (let* ((page (gethash name *pages*))
         (toc-html (with-output-to-string (ts)
                     (format ts "<div class='toc'><strong>Contents</strong><ul style='list-style:none; padding-left:0;'>")
                     (dolist (i (page-toc page))
                       (format ts "<li style='margin-left:~Dem'><a href='#~A'>~A ~A</a></li>" 
                               (* (1- (getf i :lvl)) 1.5) (getf i :id) (getf i :num) (getf i :title)))
                     (format ts "</ul></div>")))
         (bl-html (with-output-to-string (bs)
                    (format bs "<div class='backlinks'><strong>Backlinks</strong><ul>")
                    (if (page-backlinks page)
                        (dolist (l (page-backlinks page)) (format bs "<li><a href='~A.html'>~A</a></li>" l l))
                        (format bs "<li>None</li>"))
                    (format bs "</ul></div>"))))
    (multiple-value-bind (raw-html) (parse-markdown (page-content page))
      (let* ((body (regex-replace-all "\\[\\[TOC\\]\\]" raw-html toc-html))
             (body (regex-replace-all "\\[\\[BACK\\]\\]" body bl-html)))
        (with-open-file (f (merge-pathnames (make-pathname :name name :type "html") *output-dir*)
                           :direction :output :if-exists :supersede)
          (format f "<!DOCTYPE html><html><head><style>~A</style><title>~A</title></head>
                     <body><header><a href='index.html'>Home</a> | ~A</header>
                     <main>~A</main><footer>Generated ~A</footer></body></html>"
                  *style* name name body (get-universal-time)))))))

(defun sync ()
  (build-state)
  (maphash (lambda (k v) (declare (ignore v)) (render-page k)) *pages*)
  (format t "[~A] Wiki Synced (~D pages).~%" (get-universal-time) (hash-table-count *pages*)))

(defun watch ()
  (format t "Watcher started on ~A...~%" *input-dir*)
  (loop
    (let ((changed nil))
      (dolist (file (directory-files *input-dir* "*.md"))
        (let ((mtime (file-write-date file))
              (name (namestring file)))
          (unless (eql mtime (gethash name *last-mod*))
            (setf (gethash name *last-mod*) mtime)
            (setf changed t))))
      (when changed (sync))
      (sleep 1))))

(defun main ()
  (sync)
  (watch))

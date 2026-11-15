(in-package :order)

(defun parse-frontmatter (lines)
  "Parse frontmatter lines (between --- delimiters) into a plist."
  (if (and lines (string= (string-trim '(#\Space #\Tab) (first lines)) "---"))
      (let ((plist nil)
            (rest-lines (rest lines)))
        (loop for line in rest-lines for i from 0
              do (let ((trimmed (string-trim '(#\Space #\Tab) line)))
                   (if (string= trimmed "---")
                       (return-from parse-frontmatter
                         (values plist (nthcdr (1+ i) rest-lines)))
                       (let ((colon-pos (position #\: trimmed)))
                         (when colon-pos
                           (let ((key (intern (string-upcase (string-trim '(#\Space #\Tab)
                                                                          (subseq trimmed 0 colon-pos)))
                                              :keyword))
                                 (val (string-trim '(#\Space #\Tab) (subseq trimmed (1+ colon-pos)))))
                             (setf plist (append plist (list key val)))))))))
        (values plist nil))
      (values nil lines)))

(defvar *simple-inline-markup*
  '((#\` . code) (#\* . bold) (#\_ . italic)))

(defun try-parse-bracket-form (text start tag)
  "Try to parse [text](url) starting at START for the open bracket.
   Returns (values new-index (TAG text url)) or NIL."
  (let ((close-bracket (position #\] text :start start)))
    (when (and close-bracket (< (1+ close-bracket) (length text))
               (char= (char text (1+ close-bracket)) #\())
      (let ((close-paren (position #\) text :start (+ close-bracket 2))))
        (when close-paren
          (values (1+ close-paren)
                  (list tag (subseq text start close-bracket)
                        (subseq text (+ close-bracket 2) close-paren))))))))

(defun parse-inline (text)
  "Parse inline markup within TEXT, returning a list of inline elements."
  (if (or (null text) (string= text ""))
      nil
      (let ((result nil) (i 0) (len (length text))
            (special-chars (list* #\[ #\! (mapcar #'car *simple-inline-markup*))))
        (flet ((try-simple (ch)
                 (let ((entry (assoc ch *simple-inline-markup*)))
                   (when entry
                     (let ((close (position ch text :start (1+ i))))
                       (when close
                         (push (list (cdr entry) (subseq text (1+ i) close)) result)
                         (1+ close)))))))
          (loop while (< i len)
                do (let ((ch (char text i)))
                     (cond
                       ;; [[ref]] or [[ref#section]]
                       ((and (char= ch #\[) (< (1+ i) len) (char= (char text (1+ i)) #\[))
                        (let ((close (search "]]" text :start2 (+ i 2))))
                          (if close
                              (let* ((inner (subseq text (+ i 2) close))
                                     (hash-pos (position #\# inner)))
                                (push (if hash-pos
                                         `(ref ,(subseq inner 0 hash-pos) ,(subseq inner (1+ hash-pos)))
                                         `(ref ,inner))
                                      result)
                                (setf i (+ close 2)))
                              (progn (push (string ch) result) (incf i)))))
                       ;; ![alt](url)
                       ((and (char= ch #\!) (< (1+ i) len) (char= (char text (1+ i)) #\[))
                        (multiple-value-bind (new-i elem)
                            (try-parse-bracket-form text (+ i 2) 'img)
                          (if new-i
                              (progn (push elem result) (setf i new-i))
                              (progn (push (string ch) result) (incf i)))))
                       ;; [text](url)
                       ((and (char= ch #\[) (not (and (< (1+ i) len) (char= (char text (1+ i)) #\[))))
                        (multiple-value-bind (new-i elem)
                            (try-parse-bracket-form text (1+ i) 'link)
                          (if new-i
                              (progn (push elem result) (setf i new-i))
                              (progn (push (string ch) result) (incf i)))))
                       ;; `code`, *bold*, _italic_
                       ((assoc ch *simple-inline-markup*)
                        (let ((new-i (try-simple ch)))
                          (if new-i (setf i new-i)
                              (progn (push (string ch) result) (incf i)))))
                       ;; plain characters
                       (t (let ((start i))
                            (loop while (and (< i len) (not (member (char text i) special-chars)))
                                  do (incf i))
                            (when (> i start) (push (subseq text start i) result)))))))
          (let ((flat (nreverse result)))
            (if (and (= (length flat) 1) (stringp (first flat)))
                (first flat) flat))))))

(defvar *heading-prefixes*
  '(("##### " h5 6) ("#### " h4 5) ("### " h3 4) ("## " h2 3) ("# " h1 2)))

(defun try-parse-heading (trimmed)
  (dolist (entry *heading-prefixes*)
    (destructuring-bind (prefix tag offset) entry
      (when (starts-with-p trimmed prefix)
        (return (list tag (parse-inline (string-trim '(#\Space) (subseq trimmed offset)))))))))

(defun parse-line (line)
  (let ((trimmed (string-trim '(#\Space #\Tab) line)))
    (cond
      ((string= trimmed "") '(blank))
      ((and (>= (length trimmed) 3) (every (lambda (c) (char= c #\-)) trimmed)) '(hr))
      ((char= (char trimmed 0) #\()
       (handler-case (let ((*package* (find-package :order))) (read-from-string trimmed))
         (error () `(text ,trimmed))))
      ((try-parse-heading trimmed))
      ((starts-with-p trimmed "> ") `(blockquote ,(parse-inline (subseq trimmed 2))))
      ((starts-with-p trimmed "- ") `(li ,(parse-inline (subseq trimmed 2))))
      ((and (>= (length trimmed) 3) (digit-char-p (char trimmed 0))
            (let ((dot-pos (position #\. trimmed)))
              (and dot-pos (< dot-pos 4)
                   (every #'digit-char-p (subseq trimmed 0 dot-pos))
                   (> (length trimmed) (1+ dot-pos))
                   (char= (char trimmed (1+ dot-pos)) #\Space))))
       (let* ((dot-pos (position #\. trimmed))
              (num (subseq trimmed 0 dot-pos))
              (text (string-trim '(#\Space) (subseq trimmed (+ dot-pos 2)))))
         `(ol-item ,num ,(parse-inline text))))
      ((starts-with-p trimmed ":::")
       `(directive ,(string-trim '(#\Space) (subseq trimmed 3))))
      (t `(text ,(parse-inline trimmed))))))

(defun parens-balanced-p (string)
  (let ((depth 0) (in-string nil))
    (loop for c across string
          do (cond (in-string (when (char= c #\") (setf in-string nil)))
                   ((char= c #\") (setf in-string t))
                   ((char= c #\() (incf depth))
                   ((char= c #\)) (decf depth))))
    (= depth 0)))

(defun coalesce-lisp-lines (lines)
  "Merge consecutive lines forming a single multi-line s-expression."
  (let ((result nil) (accum nil))
    (dolist (line lines)
      (let ((trimmed (string-trim '(#\Space #\Tab) line)))
        (cond
          (accum
           (setf accum (concatenate 'string accum (string #\Newline) line))
           (when (parens-balanced-p accum) (push accum result) (setf accum nil)))
          ((and (> (length trimmed) 0) (char= (char trimmed 0) #\()
                (not (parens-balanced-p trimmed)))
           (setf accum line))
          (t (push line result)))))
    (when accum (push accum result))
    (nreverse result)))

(defun extract-code-blocks (lines)
  "Pre-process LINES: replace fenced code blocks with (code-block ...) forms."
  (let ((result nil) (in-code nil) (code-lang "") (code-lines nil))
    (dolist (line lines)
      (let ((trimmed (string-trim '(#\Space #\Tab) line)))
        (cond
          ((and (not in-code) (starts-with-p trimmed "```"))
           (setf in-code t code-lang (string-trim '(#\Space #\Tab) (subseq trimmed 3))
                 code-lines nil))
          ((and in-code (string= trimmed "```"))
           (let* ((content (join-strings (nreverse code-lines) (string #\Newline)))
                  (escaped (substitute-all (substitute-all content "\\" "\\\\") "\"" "\\\"")))
             (push (format nil "(code-block \"~A\" \"~A\")" code-lang escaped) result))
           (setf in-code nil))
          (in-code (push line code-lines))
          (t (push line result)))))
    (when in-code (dolist (l (nreverse code-lines)) (push l result)))
    (nreverse result)))

(defun parse-lines (lines)
  (mapcar #'parse-line (coalesce-lisp-lines (extract-code-blocks lines))))

(defun read-document (path)
  "Read and parse a .md document at PATH. Returns (values properties content-elements)."
  (let* ((text (read-file (namestring path)))
         (lines (when text (split-string text #\Newline))))
    (when lines
      (multiple-value-bind (props rest-lines) (parse-frontmatter lines)
        (values props (parse-lines (or rest-lines lines)))))))

(defun read-document-from-string (text)
  "Parse a document from a TEXT string."
  (let ((lines (split-string text #\Newline)))
    (multiple-value-bind (props rest-lines) (parse-frontmatter lines)
      (values props (parse-lines (or rest-lines lines))))))

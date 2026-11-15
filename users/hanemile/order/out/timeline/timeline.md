---
title: Timeline
type: page
visibility: public
---

# Timeline

All pages grouped by last edited date:

(timeline
  :where (lambda (o) (obj-path o))
  :show (:title :type :last-edited))

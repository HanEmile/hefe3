---
title: Shelf B
type: shelf
visibility: public
location: office
cols: 2
rows: 10
---

# Shelf B

(shelf-view "shelf-b" :cols 2 :rows 10)

## Contents

(children "shelf-b" :via "shelf" :show (:title :shelf-row :shelf-col :contains))

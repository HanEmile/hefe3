(meta
  (tag "event")
  (published "2026-11-15")
  (author "Emile")
)

# Trips

(query (where (and (tag event) (has-photos) (not (contains title Trips)))) (sort date desc) (format gallery))

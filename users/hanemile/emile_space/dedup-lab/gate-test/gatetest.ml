(* A clone that uses a top-level value defined AFTER the first
   occurrence: hoisting it must make the build fail, exercising the
   driver's revert path.  `(++)` is an operator name, so the tool
   conservatively treats it as a global instead of passing it as a
   parameter -- extracting it above its definition is unbound. *)
let a n =
  let z =
    (n ++ 3) + (n ++ 3) * 2 + (n ++ 3) + (n ++ 3)
    + (n ++ 3) + (n ++ 3) + (n ++ 3)
  in
  z * n - (z + 1) + (n ++ 3) * (n * z) + 17 + z - (n + 5) + (z - 2)

let ( ++ ) a b = a * 7 + b

let b n =
  let z =
    (n ++ 3) + (n ++ 3) * 2 + (n ++ 3) + (n ++ 3)
    + (n ++ 3) + (n ++ 3) + (n ++ 3)
  in
  z * n - (z + 1) + (n ++ 3) * (n * z) + 17 + z - (n + 5) + (z - 2)

let main () = a 1 + b 2

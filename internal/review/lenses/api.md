---
name: api
title: API endpoints and contracts
paths: **/*Controller.java, **/*Controller.kt, **/*Controller.scala, **/*Resource.java, **/controllers/**, **/routes, **/conf/routes, **/*.routes.ts, **/api/**, **/*Dto.java, **/*DTO.java, **/*Request.java, **/*Response.java
content: @RestController, @Controller, @RequestMapping, @GetMapping, @PostMapping, @PutMapping, @DeleteMapping, Action.async, express.Router, app.get(, router.post(
---
- **Validation** of every input at the boundary (`@Valid`, bean validation, schema), with a 4xx and a useful message — not a 500 from deep inside.
- **Contract**: status codes and response shapes consistent with the rest of the API; a changed DTO, route or field name is a breaking change for its clients unless the plan says so.
- **Errors**: exceptions mapped the way this API already maps them; no stack traces or internals in responses.
- **Authorization**: the new endpoint has the same access checks as its neighbours.
- **Idempotency and side effects**: PUT/DELETE retries are safe; no writes behind a GET.

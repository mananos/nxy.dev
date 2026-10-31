---
name: persistence
title: Persistence (JPA / ORM)
paths: **/*Repository.java, **/*Repository.kt, **/*Entity.java, **/entity/**, **/entities/**, **/model/**/*.java, **/*Dao.java, **/*Dao.scala
content: @Entity, @OneToMany, @ManyToOne, @ManyToMany, JpaRepository, CrudRepository, @Transactional, EntityManager, @Query
---
- **N+1**: a collection or relation loaded inside a loop, or mapped to a DTO field by field, without a fetch join / `@EntityGraph` / batch fetching.
- **Lazy loading** outside a transaction (in a controller, a mapper, a serializer) — `LazyInitializationException` waiting to happen.
- **Transactions**: `@Transactional` on the right layer (service, not controller), `readOnly` for reads, no self-invocation that skips the proxy, no long transactions around remote calls.
- **Queries**: filters and sorting in the database, not in memory; pagination for lists that grow; derived queries that match the index they need.
- **Entities**: `equals`/`hashCode` not based on generated ids or lazy relations; cascades and `orphanRemoval` intended; no entity returned by an API.

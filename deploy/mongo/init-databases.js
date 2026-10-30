// Crea el usuario de forum-service en MongoDB, con permiso de lectura y
// escritura sólo sobre la base `forum`, para que el servicio no use el
// administrador del contenedor (ADR-001, ADR-003). Mismo criterio que
// deploy/postgres/init-databases.sh.
//
// Es idempotente: lo corre el contenedor mongo-init en cada
// `docker compose up`. Si el usuario ya existe, le actualiza la contraseña y
// los permisos.
//
// Se ejecuta con mongosh conectado como el administrador del contenedor.

const user = process.env.FORUM_DB_USER;
const password = process.env.FORUM_DB_PASSWORD;
if (!user || !password) {
  print("faltan FORUM_DB_USER o FORUM_DB_PASSWORD");
  quit(1);
}
if (user === process.env.MONGO_ROOT_USER) {
  print("FORUM_DB_USER no puede ser el administrador del contenedor");
  quit(1);
}

const forum = db.getSiblingDB("forum");
const roles = [{ role: "readWrite", db: "forum" }];

if (forum.getUser(user)) {
  forum.updateUser(user, { pwd: password, roles: roles });
} else {
  forum.createUser({ user: user, pwd: password, roles: roles });
}

print(`base forum lista (usuario ${user}, sólo readWrite sobre forum)`);

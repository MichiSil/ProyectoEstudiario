// Crea en MongoDB el usuario de cada servicio, con permiso de lectura y
// escritura sólo sobre su base, para que ningún servicio use el administrador
// del contenedor (ADR-001, ADR-003). Mismo criterio que
// deploy/postgres/init-databases.sh:
//
//   - forum-service        → base `forum`,         usuario FORUM_DB_USER
//   - notification-service → base `notifications`, usuario NOTIFICATION_DB_USER
//
// Es idempotente: lo corre el contenedor mongo-init en cada
// `docker compose up`. Si el usuario ya existe, le actualiza la contraseña y
// los permisos.
//
// Se ejecuta con mongosh conectado como el administrador del contenedor.

function ensureUser(dbName, userVar, passwordVar) {
  const user = process.env[userVar];
  const password = process.env[passwordVar];
  if (!user || !password) {
    print(`faltan ${userVar} o ${passwordVar}`);
    quit(1);
  }
  if (user === process.env.MONGO_ROOT_USER) {
    print(`${userVar} no puede ser el administrador del contenedor`);
    quit(1);
  }

  const target = db.getSiblingDB(dbName);
  const roles = [{ role: "readWrite", db: dbName }];

  if (target.getUser(user)) {
    target.updateUser(user, { pwd: password, roles: roles });
  } else {
    target.createUser({ user: user, pwd: password, roles: roles });
  }

  print(`base ${dbName} lista (usuario ${user}, sólo readWrite sobre ${dbName})`);
}

ensureUser("forum", "FORUM_DB_USER", "FORUM_DB_PASSWORD");
ensureUser("notifications", "NOTIFICATION_DB_USER", "NOTIFICATION_DB_PASSWORD");

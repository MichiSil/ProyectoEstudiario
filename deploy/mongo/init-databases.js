// Crea una base y un usuario por servicio (ADR-003): ningún servicio tiene
// credenciales para la base de otro. MongoDB lo ejecuta sólo la primera vez,
// cuando el volumen de datos está vacío.

db.getSiblingDB('forum').createUser({
  user: 'forum_svc',
  pwd: process.env.FORUM_DB_PASSWORD,
  roles: [{ role: 'readWrite', db: 'forum' }],
});

db.getSiblingDB('notifications').createUser({
  user: 'notification_svc',
  pwd: process.env.NOTIFICATIONS_DB_PASSWORD,
  roles: [{ role: 'readWrite', db: 'notifications' }],
});

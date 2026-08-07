// Compatibility helpers — previously transformed Mongoose _id to id.
// With Sequelize + Postgres, models already use plain integer `id`,
// so these are simple pass-throughs kept to avoid touching every import.

export const toObjectWithId = (instance) => {
  if (!instance) return null;
  // Sequelize instances have .toJSON(), plain objects pass straight through
  return typeof instance.toJSON === 'function' ? instance.toJSON() : instance;
};

export const toObjectsWithId = (instances) => {
  if (!instances || !Array.isArray(instances)) return [];
  return instances.map(toObjectWithId);
};

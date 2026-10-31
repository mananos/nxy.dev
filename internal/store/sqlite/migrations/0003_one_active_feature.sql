UPDATE features SET done = 1 WHERE done = 0 AND EXISTS (
  SELECT 1 FROM features n WHERE n.repo = features.repo AND n.done = 0
    AND (n.updated_at > features.updated_at OR (n.updated_at = features.updated_at AND n.id > features.id)));
CREATE UNIQUE INDEX features_one_active ON features (repo) WHERE done = 0;

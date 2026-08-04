enum GoalStatus { active, completed, archived }

final class Goal {
  const Goal({
    required this.id,
    required this.title,
    required this.status,
    required this.version,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String title;
  final GoalStatus status;
  final int version;
  final DateTime createdAt;
  final DateTime updatedAt;
}

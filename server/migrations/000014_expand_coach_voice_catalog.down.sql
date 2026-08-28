BEGIN;

DO $$
DECLARE
    reviewed_binding_count INTEGER;
BEGIN
    SELECT count(*)
    INTO reviewed_binding_count
    FROM coach_voice_options
    WHERE provider = 'qianwen'
      AND provider_profile = 'qianwen_default'
      AND provider_model = 'qwen-audio-3.0-tts-flash'
      AND binding_version = 1
      AND (id, provider_voice_id) IN (
          ('voice_mary', 'loongmary'),
          ('voice_olivia', 'qwen-audio-3.0-tts-flash-loongolivialin'),
          ('voice_luna', 'qwen-audio-3.0-tts-flash-loonglunawang'),
          ('voice_nora', 'qwen-audio-3.0-tts-flash-loongnorahu'),
          ('voice_adrian', 'qwen-audio-3.0-tts-flash-loongadriangao'),
          ('voice_james', 'qwen-audio-3.0-tts-flash-loongjameszhao'),
          ('voice_ivy', 'qwen-audio-3.0-tts-flash-loongivyhu')
      );

    IF reviewed_binding_count <> 7 THEN
        RAISE EXCEPTION 'cannot roll back expanded coach voices: reviewed bindings have changed';
    END IF;

    UPDATE user_coach_presentation_preferences
    SET voice_option_id = 'voice_ava',
        version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE voice_option_id IN (
        'voice_mary',
        'voice_olivia',
        'voice_luna',
        'voice_nora',
        'voice_adrian',
        'voice_james',
        'voice_ivy'
    );

    DELETE FROM coach_voice_options
    WHERE id IN (
        'voice_mary',
        'voice_olivia',
        'voice_luna',
        'voice_nora',
        'voice_adrian',
        'voice_james',
        'voice_ivy'
    );
END
$$;

COMMIT;

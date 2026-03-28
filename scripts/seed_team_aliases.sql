-- Seed team aliases from the Go teamLabelAliases map
-- Run after enable_pg_trgm.sql and after teams table is populated

-- EPL teams
INSERT INTO team_aliases (team_id, alias_name)
SELECT t.id, a.alias FROM teams t
CROSS JOIN (VALUES
    ('man united'), ('man utd'),
    ('man city'),
    ('aston villa'),
    ('spurs'),
    ('wolves'),
    ('west ham'),
    ('newcastle'),
    ('crystal palace'),
    ('nott''m forest'), ('nottm forest'),
    ('bournemouth')
) AS a(alias)
WHERE LOWER(t.name) = CASE a.alias
    WHEN 'man united' THEN 'manchester united fc'
    WHEN 'man utd' THEN 'manchester united fc'
    WHEN 'man city' THEN 'manchester city fc'
    WHEN 'aston villa' THEN 'aston villa fc'
    WHEN 'spurs' THEN 'tottenham hotspur fc'
    WHEN 'wolves' THEN 'wolverhampton wanderers fc'
    WHEN 'west ham' THEN 'west ham united fc'
    WHEN 'newcastle' THEN 'newcastle united fc'
    WHEN 'crystal palace' THEN 'crystal palace fc'
    WHEN 'nott''m forest' THEN 'nottingham forest fc'
    WHEN 'nottm forest' THEN 'nottingham forest fc'
    WHEN 'bournemouth' THEN 'afc bournemouth'
END
ON CONFLICT (alias_name) DO NOTHING;

-- European clubs (UCL, Europa, etc.)
INSERT INTO team_aliases (team_id, alias_name)
SELECT t.id, a.alias FROM teams t
CROSS JOIN (VALUES
    ('barcelona'), ('barca'), ('barça'),
    ('bayern munich'), ('bayern'), ('bayern münchen'),
    ('real madrid'),
    ('juventus'), ('juve'),
    ('psg'), ('paris saint-germain'),
    ('inter milan'), ('inter'), ('internazionale'),
    ('atletico madrid'), ('atletico'), ('atlético madrid'), ('atlético'),
    ('dortmund'), ('borussia dortmund'),
    ('galatasaray'),
    ('benfica'),
    ('bayer leverkusen'), ('leverkusen'),
    ('porto'),
    ('monaco'),
    ('salzburg'), ('red bull salzburg'),
    ('ajax'),
    ('roma'),
    ('atalanta'),
    ('young boys'),
    ('club brugge'),
    ('celtic'),
    ('feyenoord'),
    ('psv'),
    ('sporting'), ('sporting cp'),
    ('napoli'),
    ('lazio'),
    ('sevilla'),
    ('villarreal'),
    ('marseille'),
    ('lyon'),
    ('lille'),
    ('leipzig'), ('rb leipzig'),
    ('stuttgart'),
    ('wolfsburg'),
    ('bruges'),
    ('girona'),
    ('bologna'),
    ('slovan bratislava'),
    ('red star belgrade'),
    ('dinamo zagreb'),
    ('shakhtar donetsk'), ('shakhtar')
) AS a(alias)
WHERE LOWER(t.name) = CASE a.alias
    WHEN 'barcelona' THEN 'fc barcelona'
    WHEN 'barca' THEN 'fc barcelona'
    WHEN 'barça' THEN 'fc barcelona'
    WHEN 'bayern munich' THEN 'fc bayern münchen'
    WHEN 'bayern' THEN 'fc bayern münchen'
    WHEN 'bayern münchen' THEN 'fc bayern münchen'
    WHEN 'real madrid' THEN 'real madrid cf'
    WHEN 'juventus' THEN 'juventus fc'
    WHEN 'juve' THEN 'juventus fc'
    WHEN 'psg' THEN 'paris saint-germain fc'
    WHEN 'paris saint-germain' THEN 'paris saint-germain fc'
    WHEN 'inter milan' THEN 'fc internazionale milano'
    WHEN 'inter' THEN 'fc internazionale milano'
    WHEN 'internazionale' THEN 'fc internazionale milano'
    WHEN 'atletico madrid' THEN 'club atlético de madrid'
    WHEN 'atletico' THEN 'club atlético de madrid'
    WHEN 'atlético madrid' THEN 'club atlético de madrid'
    WHEN 'atlético' THEN 'club atlético de madrid'
    WHEN 'dortmund' THEN 'bv borussia 09 dortmund'
    WHEN 'borussia dortmund' THEN 'bv borussia 09 dortmund'
    WHEN 'galatasaray' THEN 'galatasaray sk'
    WHEN 'benfica' THEN 'sport lisboa e benfica'
    WHEN 'bayer leverkusen' THEN 'bayer 04 leverkusen'
    WHEN 'leverkusen' THEN 'bayer 04 leverkusen'
    WHEN 'porto' THEN 'fc porto'
    WHEN 'monaco' THEN 'as monaco fc'
    WHEN 'salzburg' THEN 'fc red bull salzburg'
    WHEN 'red bull salzburg' THEN 'fc red bull salzburg'
    WHEN 'ajax' THEN 'afc ajax'
    WHEN 'roma' THEN 'as roma'
    WHEN 'atalanta' THEN 'atalanta bc'
    WHEN 'young boys' THEN 'bsc young boys'
    WHEN 'club brugge' THEN 'club brugge kv'
    WHEN 'celtic' THEN 'celtic fc'
    WHEN 'feyenoord' THEN 'feyenoord rotterdam'
    WHEN 'psv' THEN 'psv eindhoven'
    WHEN 'sporting' THEN 'sporting clube de portugal'
    WHEN 'sporting cp' THEN 'sporting clube de portugal'
    WHEN 'napoli' THEN 'ssc napoli'
    WHEN 'lazio' THEN 'ss lazio'
    WHEN 'sevilla' THEN 'sevilla fc'
    WHEN 'villarreal' THEN 'villarreal cf'
    WHEN 'marseille' THEN 'olympique de marseille'
    WHEN 'lyon' THEN 'olympique lyonnais'
    WHEN 'lille' THEN 'losc lille'
    WHEN 'leipzig' THEN 'rb leipzig'
    WHEN 'rb leipzig' THEN 'rasenballsport leipzig'
    WHEN 'stuttgart' THEN 'vfb stuttgart'
    WHEN 'wolfsburg' THEN 'vfl wolfsburg'
    WHEN 'bruges' THEN 'club brugge kv'
    WHEN 'girona' THEN 'girona fc'
    WHEN 'bologna' THEN 'bologna fc 1909'
    WHEN 'slovan bratislava' THEN 'šk slovan bratislava'
    WHEN 'red star belgrade' THEN 'fk crvena zvezda'
    WHEN 'dinamo zagreb' THEN 'gnk dinamo zagreb'
    WHEN 'shakhtar donetsk' THEN 'fc shakhtar donetsk'
    WHEN 'shakhtar' THEN 'fc shakhtar donetsk'
END
ON CONFLICT (alias_name) DO NOTHING;

-- Generate soccer suffix/prefix aliases automatically
-- For teams ending with ' FC', add alias without ' FC'
INSERT INTO team_aliases (team_id, alias_name)
SELECT id, LOWER(TRIM(TRAILING ' fc' FROM name))
FROM teams
WHERE LOWER(name) LIKE '% fc'
  AND LOWER(TRIM(TRAILING ' fc' FROM name)) != LOWER(name)
ON CONFLICT (alias_name) DO NOTHING;

-- For teams starting with 'FC ', add alias without 'FC '
INSERT INTO team_aliases (team_id, alias_name)
SELECT id, LOWER(TRIM(LEADING 'fc ' FROM name))
FROM teams
WHERE LOWER(name) LIKE 'fc %'
  AND LOWER(TRIM(LEADING 'fc ' FROM name)) != LOWER(name)
ON CONFLICT (alias_name) DO NOTHING;

-- Add other common soccer prefixes
INSERT INTO team_aliases (team_id, alias_name)
SELECT id, LOWER(REPLACE(REPLACE(REPLACE(name, 'AFC ', ''), 'BSC ', ''), 'AS ', ''))
FROM teams
WHERE LOWER(name) LIKE 'afc %' OR LOWER(name) LIKE 'bsc %' OR LOWER(name) LIKE 'as %'
ON CONFLICT (alias_name) DO NOTHING;
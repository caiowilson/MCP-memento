<?php
declare(strict_types=1);

namespace VirelliumK9\Domain;

enum GlintPhase: string
{
    case Seeded = 'seeded';
    case Tempering = 'tempering';
    case Sealed = 'sealed';
}

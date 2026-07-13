<?php

declare(strict_types=1);

namespace App\Entity;

use App\Repository\ReportRepository;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity(repositoryClass: ReportRepository::class)]
final class Report
{
    #[ORM\Id]
    #[ORM\Column]
    private int $id;
}

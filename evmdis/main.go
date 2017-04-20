package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/Arachnid/evmdis"
	"io/ioutil"
	"log"
	"os"
)

const swarmHashLength = 43

var swarmHashProgramTrailer = [...]byte{0x00, 0x29}
var swarmHashHeader = [...]byte{0xa1, 0x65}

func main() {

	withSwarmHash := flag.Bool("swarm", true, "solc adds a reference to the Swarm API description to the generated bytecode, if this flag is set it removes this reference before analysis")
	ctorMode := flag.Bool("ctor", false, "Indicates that the provided bytecode has construction(ctor) code included. (needs to be analyzed seperatly)")
	logging := flag.Bool("log", false, "print logging output")

	flag.Parse()

	if !*logging {
		log.SetOutput(ioutil.Discard)
	}

	hexdata, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(fmt.Sprintf("Could not read from stdin: %v", err))
	}

	bytecodeLength := uint64(hex.DecodedLen(len(hexdata)))
	bytecode := make([]byte, bytecodeLength)

	hex.Decode(bytecode, hexdata)

	// detect swarm hash and remove it from bytecode, see http://solidity.readthedocs.io/en/latest/miscellaneous.html?highlight=swarm#encoding-of-the-metadata-hash-in-the-bytecode
	if bytecode[bytecodeLength-1] == swarmHashProgramTrailer[1] &&
		bytecode[bytecodeLength-2] == swarmHashProgramTrailer[0] &&
		bytecode[bytecodeLength-43] == swarmHashHeader[0] &&
		bytecode[bytecodeLength-42] == swarmHashHeader[1] &&
		*withSwarmHash {
		bytecodeLength -= swarmHashLength // remove swarm part
	}

	program := evmdis.NewProgram(bytecode[:bytecodeLength])
	AnalyzeProgram(program)

	if *ctorMode {
		var codeEntryPoint = FindNextCodeEntryPoint(program)

		if codeEntryPoint == 0 {
			panic("no code entrypoint found in ctor")
		} else if codeEntryPoint >= bytecodeLength {
			panic("code entrypoint outside of currently available code")
		}

		ctor := evmdis.NewProgram(bytecode[:codeEntryPoint])
		code := evmdis.NewProgram(bytecode[codeEntryPoint:bytecodeLength])

		AnalyzeProgram(ctor)
		fmt.Println("# Constructor part -------------------------")
		PrintAnalysisResult(ctor)

		AnalyzeProgram(code)
		fmt.Println("# Code part -------------------------")
		PrintAnalysisResult(code)

	} else {
		PrintAnalysisResult(program)
	}
}

func FindNextCodeEntryPoint(program *evmdis.Program) uint64 {
	var lastPos uint64 = 0
	for _, block := range program.Blocks {
		for _, instruction := range block.Instructions {
			if instruction.Op == evmdis.CODECOPY {
				var expression evmdis.Expression

				instruction.Annotations.Get(&expression)

				arg := expression.(*evmdis.InstructionExpression).Arguments[1].Eval()

				if arg != nil {
					lastPos = arg.Uint64()
				}
			}
		}
	}
	return lastPos
}

func PrintAnalysisResult(program *evmdis.Program) {
	for _, block := range program.Blocks {
		offset := block.Offset

		// Print out the jump label for the block, if there is one
		var label *evmdis.JumpLabel
		block.Annotations.Get(&label)
		if label != nil {
			fmt.Printf("%v\n", label)
		}

		// Print out the stack prestate for this block
		var reaching evmdis.ReachingDefinition
		block.Annotations.Get(&reaching)
		fmt.Printf("# Stack: %v\n", reaching)

		for _, instruction := range block.Instructions {
			var expression evmdis.Expression
			instruction.Annotations.Get(&expression)

			if expression != nil {
				if instruction.Op.StackWrites() == 1 && !instruction.Op.IsDup() {
					fmt.Printf("0x%X\tPUSH(%v)\n", offset, expression)
				} else {
					fmt.Printf("0x%X\t%v\n", offset, expression)
				}
			}
			offset += instruction.Op.OperandSize() + 1
		}
		fmt.Printf("\n")
	}
}

func AnalyzeProgram(program *evmdis.Program) {
	if err := evmdis.PerformReachingAnalysis(program); err != nil {
		panic(fmt.Sprintf("Error performing reaching analysis: %v", err))
	}
	evmdis.PerformReachesAnalysis(program)
	evmdis.CreateLabels(program)
	if err := evmdis.BuildExpressions(program); err != nil {
		panic(fmt.Sprintf("Error building expressions: %v", err))
	}
}
